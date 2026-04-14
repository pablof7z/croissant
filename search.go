package main

import (
	"os"
	"path/filepath"
	"strings"

	"fiatjaf.com/nostr"
	_ "github.com/blevesearch/bleve/v2/analysis/analyzer/simple"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ar"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/cjk"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/da"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/de"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/es"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fa"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fi"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/fr"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/gl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hi"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hr"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/hu"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/in"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/it"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/nl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/no"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/pl"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/pt"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ro"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ru"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/sv"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/tr"
	"github.com/pemistahl/lingua-go"
)

const (
	labelContentField = "c"

	labelKindField       = "k"
	labelCreatedAtField  = "a"
	labelAuthorField     = "p"
	labelReferencesField = "r"
	labelExtrasField     = "x"
)

const languageFileName = "lang"

var (
	indexableKinds = []nostr.Kind{0, 1, 6, 9, 11, 16, 20, 21, 22, 24, 1111, 9802, 30023, 30818}

	detector lingua.LanguageDetector
)

func detectLanguage(content string) lingua.Language {
	if detector != nil {
		if lang, ok := detector.DetectLanguageOf(content); ok {
			return lang
		}
	}

	return lingua.English
}

func analyzerFromLangCode(langCode string) string {
	switch strings.ToLower(langCode) {
	case "ja", "zh", "ko":
		return "cjk"
	default:
		if langCode == "" {
			return "en"
		}
		return strings.ToLower(langCode)
	}
}

func readLanguage(indexPath string) (string, bool, error) {
	data, err := os.ReadFile(filepath.Join(indexPath, languageFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}

	lang := strings.TrimSpace(string(data))
	if lang == "" {
		return "", false, nil
	}

	return lang, true, nil
}

func writeLanguage(indexPath string, langCode string) error {
	return os.WriteFile(filepath.Join(indexPath, languageFileName), []byte(langCode+"\n"), 0644)
}
