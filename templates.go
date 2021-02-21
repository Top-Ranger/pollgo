package main

import (
	"embed"
	"html/template"
)

//go:embed template
var templateFiles embed.FS
var textTemplate *template.Template

type textTemplateStruct struct {
	Text        template.HTML
	Translation Translation
	ServerPath  string
}

func init() {
	var err error

	textTemplate, err = template.ParseFS(templateFiles, "template/text.html")
	if err != nil {
		panic(err)
	}
}
