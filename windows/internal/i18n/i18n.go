package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"golang.org/x/sys/windows"
	"pulse-windows/internal/model"
)

var activeLang atomic.Value // stores string: "zh-CN" or "en"

func init() {
	activeLang.Store(detectSystemLang())
}

func SetLanguage(lang model.Language) {
	switch lang {
	case model.LangZhCN:
		activeLang.Store("zh-CN")
	case model.LangEn:
		activeLang.Store("en")
	default:
		activeLang.Store(detectSystemLang())
	}
}

func Current() string {
	v, _ := activeLang.Load().(string)
	if v == "" {
		return "en"
	}
	return v
}

func IsChinese() bool {
	return Current() == "zh-CN"
}

func detectSystemLang() string {
	// Check Windows UI language via GetUserDefaultUILanguage
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")
	if err := proc.Find(); err == nil {
		r, _, _ := proc.Call()
		langID := uint16(r) & 0x3FF // primary language ID
		if langID == 0x04 {          // LANG_CHINESE
			return "zh-CN"
		}
	}
	env := strings.ToLower(os.Getenv("LANG"))
	if strings.HasPrefix(env, "zh") {
		return "zh-CN"
	}
	return "en"
}

func T(en, zh string) string {
	if IsChinese() {
		return zh
	}
	return en
}

func TF(en, zh string, args ...any) string {
	if IsChinese() {
		return fmt.Sprintf(zh, args...)
	}
	return fmt.Sprintf(en, args...)
}
