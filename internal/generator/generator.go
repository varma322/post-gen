package generator

import (
	"bytes"
	"os"
	"post-gen/internal/models"
	"sync"
	"text/template"
)

var (
	tmplCache  = make(map[string]*template.Template)
	cacheMutex sync.RWMutex
)

// postFuncs are the helpers templates may call. Kept deliberately small: a
// template is account-owned content edited through the UI, so every function
// exposed here is one more thing a non-programmer can get wrong.
var postFuncs = template.FuncMap{
	"money": FormatMoney,
}

// GeneratePost takes a product and a template path, and returns the rendered string.
// Parsed templates are cached by path to avoid redundant disk I/O during bulk runs.
func GeneratePost(product models.Product, templatePath string) (string, error) {
	cacheMutex.RLock()
	tmpl, exists := tmplCache[templatePath]
	cacheMutex.RUnlock()

	if !exists {
		tmplData, err := os.ReadFile(templatePath)
		if err != nil {
			return "", err
		}

		newTmpl, err := template.New("post").Funcs(postFuncs).Parse(string(tmplData))
		if err != nil {
			return "", err
		}

		cacheMutex.Lock()
		if tmpl, exists = tmplCache[templatePath]; !exists {
			tmplCache[templatePath] = newTmpl
			tmpl = newTmpl
		}
		cacheMutex.Unlock()
	}

	var buf bytes.Buffer
	err := tmpl.Execute(&buf, product)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// Validate reports whether source parses as a post template. It must use the
// same FuncMap as GeneratePost, or the API would reject a template on save
// that the renderer would have handled - and accept one it cannot render.
func Validate(name, source string) error {
	_, err := template.New(name).Funcs(postFuncs).Parse(source)
	return err
}

// InvalidateCache removes a template from the in-memory caches by its path.
// This must be called whenever a template file is updated on disk (e.g. via the API)
// to ensure subsequent renders pick up the new content.
func InvalidateCache(templatePath string) {
	cacheMutex.Lock()
	delete(tmplCache, templatePath)
	cacheMutex.Unlock()

	// The profile is derived from the same source, so it goes stale at exactly
	// the same moment - an edited template must be re-profiled before the AI
	// enricher builds its next prompt from it.
	profileCacheMu.Lock()
	delete(profileCache, templatePath)
	profileCacheMu.Unlock()
}
