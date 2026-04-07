package generator

import (
	"bytes"
	"os"
	"post-gen/internal/models"
	"text/template"
)

// GeneratePost takes a product and a template path, and returns the rendered string.
func GeneratePost(product models.Product, templatePath string) (string, error) {
	tmplData, err := os.ReadFile(templatePath)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("post").Parse(string(tmplData))
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, product)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
