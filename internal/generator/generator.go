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

		tmpl, err = template.New("post").Parse(string(tmplData))
		if err != nil {
			return "", err
		}

		cacheMutex.Lock()
		tmplCache[templatePath] = tmpl
		cacheMutex.Unlock()
	}

	var buf bytes.Buffer
	err := tmpl.Execute(&buf, product)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

// InvalidateCache removes a template from the in-memory cache by its path.
// This must be called whenever a template file is updated on disk (e.g. via the API)
// to ensure subsequent renders pick up the new content.
func InvalidateCache(templatePath string) {
	cacheMutex.Lock()
	delete(tmplCache, templatePath)
	cacheMutex.Unlock()
}
