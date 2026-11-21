package tui

// FooterMenu is an inline footer menu (never huh overlay).
type FooterMenu int

const (
	MenuNone FooterMenu = iota
	MenuActions
	MenuRoles
	MenuView
	MenuModels
	MenuChats
)

const (
	ActionNewChat = iota
	ActionModels
	ActionConnect
	ActionChats
	ActionTeams
	ActionWorkDir
	ActionExit
)

const addModelID = "__add_model__"

var actionLabels = []string{
	"Start New Chat",
	"Models",
	"Connect API",
	"Switch / List Chats",
	"Show Teams",
	"Work on path",
	"Exit",
}

var connectLabels = []string{
	"Name",
	"API URL",
	"API key",
	"Model",
	"Prompt price / 1M",
	"Completion price / 1M",
}

var addModelLabels = []string{
	"Model",
	"Prompt price / 1M",
	"Completion price / 1M",
}

func modelLabel(opt ModelOption) string {
	if opt.URL == "" {
		return opt.ID
	}
	return opt.ID + " · " + opt.URL
}

// MenuTrigger reports whether a compose draft may open a footer menu.
func MenuTrigger(draft string, key rune) FooterMenu {
	if draft != "" {
		return MenuNone
	}
	switch key {
	case '/':
		return MenuActions
	case '*':
		return MenuRoles
	case '=':
		return MenuView
	default:
		return MenuNone
	}
}

// MentionInsert formats catalog display name with trailing space.
func MentionInsert(displayName string) string {
	return "*" + displayName + " "
}

// MentionFallback converts a bare key after closing the picker into normal text.
func MentionFallback(key rune) string {
	return "*" + string(key)
}
