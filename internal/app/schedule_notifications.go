package app

import (
	"fmt"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
)

type scheduleNoticeKind string

const (
	scheduleNoticeInvalidJSON        scheduleNoticeKind = "invalid_json"
	scheduleNoticeInvalidSchema      scheduleNoticeKind = "invalid_schema"
	scheduleNoticeProjectUnavailable scheduleNoticeKind = "project_unavailable"
	scheduleNoticeMissed             scheduleNoticeKind = "missed"
	scheduleNoticeRefused            scheduleNoticeKind = "refused"
)

type scheduleLanguage interface {
	ProductLanguage() string
}

func (b *ScheduleBook) notificationLanguage() string {
	if source, ok := b.Broker.(scheduleLanguage); ok {
		if language := source.ProductLanguage(); language != "" {
			return language
		}
	}
	return "en"
}

func invalidScheduleNoticeKind(kind string) scheduleNoticeKind {
	switch kind {
	case "unreadable_json":
		return scheduleNoticeInvalidJSON
	case "project_unavailable":
		return scheduleNoticeProjectUnavailable
	default:
		return scheduleNoticeInvalidSchema
	}
}

// scheduleNotice is the daemon-side catalog for schedule pushes. PushSend's
// boundary is already rendered title/body text, so the receiver has no stable
// code-and-arguments envelope to localize. Producer detail therefore never
// enters this function; only a classified kind and its safe parameter do.
func scheduleNotice(language string, kind scheduleNoticeKind, value string) string {
	locale := nextconfig.ResolveProductLanguage(language)
	templates, ok := scheduleNotices[locale]
	if !ok || !validScheduleNoticeCatalog(locale) {
		templates = scheduleNotices["en"]
	}
	template, ok := templates[kind]
	if !ok || template == "" {
		// Future secondary-language additions may be batched. A missing key
		// falls back to the English message with the same stable kind.
		template, ok = scheduleNotices["en"][kind]
		if !ok {
			template = scheduleNotices["en"][scheduleNoticeInvalidSchema]
		}
	}
	if kind == scheduleNoticeMissed {
		return template
	}
	return fmt.Sprintf(template, value)
}

func scheduleNoticeCoverage(language string) (translated, english int) {
	english = len(scheduleNotices["en"])
	locale := nextconfig.ResolveProductLanguage(language)
	if !validScheduleNoticeCatalog(locale) {
		return 0, english
	}
	for kind, template := range scheduleNotices[locale] {
		if _, known := scheduleNotices["en"][kind]; known && template != "" {
			translated++
		}
	}
	return translated, english
}

func validScheduleNoticeCatalog(language string) bool {
	templates, ok := scheduleNotices[language]
	if !ok {
		return false
	}
	for kind, translated := range templates {
		english, exists := scheduleNotices["en"][kind]
		if !exists || translated == "" {
			return false
		}
		englishVerbs, englishValid := scheduleNoticeVerbs(english)
		translatedVerbs, translatedValid := scheduleNoticeVerbs(translated)
		if !englishValid || !translatedValid || englishVerbs != translatedVerbs {
			return false
		}
	}
	if language == "zh-Hant" && len(templates) != len(scheduleNotices["en"]) {
		return false
	}
	return true
}

// The schedule templates take at most one string parameter. A literal percent
// must be escaped as %%, so a translation cannot introduce another fmt verb.
func scheduleNoticeVerbs(template string) (int, bool) {
	verbs := 0
	for i := 0; i < len(template); i++ {
		if template[i] != '%' {
			continue
		}
		i++
		if i == len(template) {
			return 0, false
		}
		switch template[i] {
		case '%':
		case 's':
			verbs++
		default:
			return 0, false
		}
	}
	return verbs, true
}

// Stable notice kinds, with every shipped product language represented. The
// value is a filename or an error code, never producer-provided error detail.
var scheduleNotices = map[string]map[scheduleNoticeKind]string{
	"en": {
		scheduleNoticeInvalidJSON:        "Schedule file %s could not be read and was disabled.",
		scheduleNoticeInvalidSchema:      "Schedule file %s has an invalid format and was disabled.",
		scheduleNoticeProjectUnavailable: "Schedule file %s names a project directory that is currently unavailable and was disabled.",
		scheduleNoticeMissed:             "The scheduled run missed its catch-up window and did not start.",
		scheduleNoticeRefused:            "The scheduled run could not start (%s).",
	},
	"zh-Hant": {
		scheduleNoticeInvalidJSON:        "無法讀取排程檔 %s，已停用。",
		scheduleNoticeInvalidSchema:      "排程檔 %s 的內容不符合格式，已停用。",
		scheduleNoticeProjectUnavailable: "排程檔 %s 指定的專案資料夾目前無法使用，已停用。",
		scheduleNoticeMissed:             "排程執行已超過可補跑的時間，這次沒有啟動。",
		scheduleNoticeRefused:            "排程這次無法啟動（%s）。",
	},
	"ja": {
		scheduleNoticeInvalidJSON:        "スケジュールファイル %s を読み取れないため、無効にしました。",
		scheduleNoticeInvalidSchema:      "スケジュールファイル %s の形式が無効なため、無効にしました。",
		scheduleNoticeProjectUnavailable: "スケジュールファイル %s で指定されたプロジェクトフォルダーを利用できないため、無効にしました。",
		scheduleNoticeMissed:             "予定された実行は再実行可能な時間を過ぎたため、開始されませんでした。",
		scheduleNoticeRefused:            "予定された実行を開始できませんでした（%s）。",
	},
	"zh-Hans": {
		scheduleNoticeInvalidJSON:        "无法读取排程文件 %s，已停用。",
		scheduleNoticeInvalidSchema:      "排程文件 %s 的格式无效，已停用。",
		scheduleNoticeProjectUnavailable: "排程文件 %s 指定的项目文件夹目前不可用，已停用。",
		scheduleNoticeMissed:             "排程执行已超过可补跑时间，本次未启动。",
		scheduleNoticeRefused:            "本次排程无法启动（%s）。",
	},
	"ko": {
		scheduleNoticeInvalidJSON:        "일정 파일 %s을(를) 읽을 수 없어 비활성화했습니다.",
		scheduleNoticeInvalidSchema:      "일정 파일 %s의 형식이 올바르지 않아 비활성화했습니다.",
		scheduleNoticeProjectUnavailable: "일정 파일 %s에 지정된 프로젝트 폴더를 사용할 수 없어 비활성화했습니다.",
		scheduleNoticeMissed:             "예약된 실행의 재시도 가능 시간이 지나 시작하지 않았습니다.",
		scheduleNoticeRefused:            "예약된 실행을 시작할 수 없습니다(%s).",
	},
	"es": {
		scheduleNoticeInvalidJSON:        "No se pudo leer el archivo de programación %s y se desactivó.",
		scheduleNoticeInvalidSchema:      "El archivo de programación %s tiene un formato no válido y se desactivó.",
		scheduleNoticeProjectUnavailable: "La carpeta del proyecto indicada en el archivo de programación %s no está disponible y se desactivó.",
		scheduleNoticeMissed:             "La ejecución programada superó el plazo de recuperación y no se inició.",
		scheduleNoticeRefused:            "No se pudo iniciar la ejecución programada (%s).",
	},
	"pt-BR": {
		scheduleNoticeInvalidJSON:        "Não foi possível ler o arquivo de agendamento %s; ele foi desativado.",
		scheduleNoticeInvalidSchema:      "O arquivo de agendamento %s tem formato inválido e foi desativado.",
		scheduleNoticeProjectUnavailable: "A pasta do projeto indicada no arquivo de agendamento %s está indisponível; o arquivo foi desativado.",
		scheduleNoticeMissed:             "A execução agendada perdeu o prazo de recuperação e não foi iniciada.",
		scheduleNoticeRefused:            "Não foi possível iniciar a execução agendada (%s).",
	},
	"fr": {
		scheduleNoticeInvalidJSON:        "Impossible de lire le fichier de planification %s ; il a été désactivé.",
		scheduleNoticeInvalidSchema:      "Le fichier de planification %s a un format invalide et a été désactivé.",
		scheduleNoticeProjectUnavailable: "Le dossier de projet indiqué dans le fichier de planification %s est indisponible ; le fichier a été désactivé.",
		scheduleNoticeMissed:             "L’exécution planifiée a dépassé sa période de rattrapage et n’a pas démarré.",
		scheduleNoticeRefused:            "Impossible de démarrer l’exécution planifiée (%s).",
	},
	"de": {
		scheduleNoticeInvalidJSON:        "Die Zeitplandatei %s konnte nicht gelesen werden und wurde deaktiviert.",
		scheduleNoticeInvalidSchema:      "Die Zeitplandatei %s hat ein ungültiges Format und wurde deaktiviert.",
		scheduleNoticeProjectUnavailable: "Der in der Zeitplandatei %s angegebene Projektordner ist nicht verfügbar; die Datei wurde deaktiviert.",
		scheduleNoticeMissed:             "Der geplante Lauf lag außerhalb des Nachholzeitfensters und wurde nicht gestartet.",
		scheduleNoticeRefused:            "Der geplante Lauf konnte nicht gestartet werden (%s).",
	},
}
