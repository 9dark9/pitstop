package options

import (
	"slices"
	"strings"

	"github.com/Bush2021/pitstop/locales"
)

// Where the full lists are. GitHub, as chromium.googlesource.com doesn't
// open for every user.
const (
	CommandsURL = "https://github.com/chromium/chromium/blob/main/chrome/app/chrome_command_ids.h"
	SwitchesURL = "https://github.com/chromium/chromium/blob/main/chrome/common/chrome_switches.h"
)

// Entry is a line of a reference list: what it does, in words, and the
// value it writes.
type Entry struct {
	Group, Label Text
	Value        string
	// Detail is the name Chromium's source knows it by, if not Value.
	Detail string
}

func (e Entry) Matches(query string, langs ...locales.Messages) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if strings.Contains(strings.ToLower(e.Value), q) || strings.Contains(strings.ToLower(e.Detail), q) {
		return true
	}
	for _, m := range langs {
		if strings.Contains(strings.ToLower(e.Label(m)), q) {
			return true
		}
	}
	return false
}

// CommandLabel returns the words for a command ID, if the list has it.
func CommandLabel(id string, m locales.Messages) (string, bool) {
	for _, e := range ChromeCommands {
		if e.Value == id {
			return e.Label(m), true
		}
	}
	return "", false
}

func group(g Text, entries ...Entry) []Entry {
	for i := range entries {
		entries[i].Group = g
	}
	return entries
}

// ChromeCommands are the commands a key mapping is likely to run, with
// their IDs in chrome/app/chrome_command_ids.h.
var ChromeCommands = func() []Entry {
	type m = locales.Messages
	cmd := func(id, name string, label Text) Entry { return Entry{Label: label, Value: id, Detail: name} }
	return slices.Concat(
		group(m.GroupTabs,
			cmd("34014", "IDC_NEW_TAB", m.CmdNewTab),
			cmd("34015", "IDC_CLOSE_TAB", m.CmdCloseTab),
			cmd("34028", "IDC_RESTORE_TAB", m.CmdRestoreTab),
			cmd("34027", "IDC_DUPLICATE_TAB", m.CmdDuplicateTab),
			cmd("34016", "IDC_SELECT_NEXT_TAB", m.CmdNextTab),
			cmd("34017", "IDC_SELECT_PREVIOUS_TAB", m.CmdPreviousTab),
			cmd("34018", "IDC_SELECT_TAB_0", m.CmdFirstTab),
			cmd("34026", "IDC_SELECT_LAST_TAB", m.CmdLastTab),
			cmd("34032", "IDC_MOVE_TAB_NEXT", m.CmdMoveTabRight),
			cmd("34033", "IDC_MOVE_TAB_PREVIOUS", m.CmdMoveTabLeft),
			cmd("34056", "IDC_MOVE_TAB_TO_NEW_WINDOW", m.CmdMoveTabToWindow),
			cmd("35013", "IDC_WINDOW_PIN_TAB", m.CmdPinTab),
			cmd("35012", "IDC_WINDOW_MUTE_SITE", m.CmdMuteSite),
			cmd("35023", "IDC_WINDOW_CLOSE_OTHER_TABS", m.CmdCloseOtherTabs),
			cmd("35022", "IDC_WINDOW_CLOSE_TABS_TO_RIGHT", m.CmdCloseTabsRight),
			cmd("52500", "IDC_TAB_SEARCH", m.CmdTabSearch),
		),
		group(m.GroupWindows,
			cmd("34000", "IDC_NEW_WINDOW", m.CmdNewWindow),
			cmd("34001", "IDC_NEW_INCOGNITO_WINDOW", m.CmdNewIncognito),
			cmd("34012", "IDC_CLOSE_WINDOW", m.CmdCloseWindow),
			cmd("34046", "IDC_MINIMIZE_WINDOW", m.CmdMinimize),
			cmd("34047", "IDC_MAXIMIZE_WINDOW", m.CmdMaximize),
			cmd("34030", "IDC_FULLSCREEN", m.CmdFullscreen),
			cmd("34031", "IDC_EXIT", m.CmdExit),
		),
		group(m.GroupNavigation,
			cmd("33000", "IDC_BACK", m.CmdBack),
			cmd("33001", "IDC_FORWARD", m.CmdForward),
			cmd("33002", "IDC_RELOAD", m.CmdReload),
			cmd("33007", "IDC_RELOAD_BYPASSING_CACHE", m.CmdHardReload),
			cmd("33006", "IDC_STOP", m.CmdStop),
			cmd("33003", "IDC_HOME", m.CmdHome),
			cmd("39001", "IDC_FOCUS_LOCATION", m.CmdFocusAddress),
		),
		group(m.GroupPage,
			cmd("37000", "IDC_FIND", m.CmdFind),
			cmd("35003", "IDC_PRINT", m.CmdPrint),
			cmd("35004", "IDC_SAVE_PAGE", m.CmdSavePage),
			cmd("35000", "IDC_BOOKMARK_THIS_TAB", m.CmdBookmark),
			cmd("35009", "IDC_SHOW_TRANSLATE", m.CmdTranslate),
			cmd("34071", "IDC_COPY_URL", m.CmdCopyURL),
			cmd("38001", "IDC_ZOOM_PLUS", m.CmdZoomIn),
			cmd("38003", "IDC_ZOOM_MINUS", m.CmdZoomOut),
			cmd("38002", "IDC_ZOOM_NORMAL", m.CmdZoomReset),
			cmd("35002", "IDC_VIEW_SOURCE", m.CmdViewSource),
		),
		group(m.GroupChrome,
			cmd("40015", "IDC_OPTIONS", m.CmdSettings),
			cmd("40010", "IDC_SHOW_HISTORY", m.CmdHistory),
			cmd("40012", "IDC_SHOW_DOWNLOADS", m.CmdDownloads),
			cmd("40011", "IDC_SHOW_BOOKMARK_MANAGER", m.CmdBookmarks),
			cmd("40009", "IDC_SHOW_BOOKMARK_BAR", m.CmdBookmarkBar),
			cmd("40022", "IDC_MANAGE_EXTENSIONS", m.CmdExtensions),
			cmd("35041", "IDC_SHOW_PASSWORD_MANAGER", m.CmdPasswords),
			cmd("40013", "IDC_CLEAR_BROWSING_DATA", m.CmdClearData),
			cmd("40006", "IDC_TASK_MANAGER", m.CmdTaskManager),
			cmd("40004", "IDC_DEV_TOOLS", m.CmdDevTools),
			cmd("40021", "IDC_SHOW_APP_MENU", m.CmdAppMenu),
			cmd("40018", "IDC_ABOUT", m.CmdAboutChrome),
		),
	)
}()

// ChromeShortcut returns what a shortcut does in Chrome by default, if it
// does anything.
func ChromeShortcut(hotkey string, m locales.Messages) (string, bool) {
	if label, ok := chromeKeys[Chord(hotkey)]; ok {
		return label(m), true
	}
	return "", false
}

// chromeKeys are Chrome's shortcuts on Windows, from
// chrome/browser/ui/accelerator_table.cc, by Chord.
var chromeKeys = func() map[string]Text {
	type m = locales.Messages
	keys := map[string]Text{}
	add := func(label Text, hotkeys ...string) {
		for _, k := range hotkeys {
			keys[Chord(k)] = label
		}
	}
	add(m.CmdNewTab, "Ctrl+T")
	add(m.CmdCloseTab, "Ctrl+W", "Ctrl+F4")
	add(m.CmdRestoreTab, "Ctrl+Shift+T")
	add(m.CmdNextTab, "Ctrl+Tab", "Ctrl+PageDown")
	add(m.CmdPreviousTab, "Ctrl+Shift+Tab", "Ctrl+PageUp")
	add(m.CmdFirstTab, "Ctrl+1")
	add(m.CmdTabByPosition, "Ctrl+2", "Ctrl+3", "Ctrl+4", "Ctrl+5", "Ctrl+6", "Ctrl+7", "Ctrl+8")
	add(m.CmdLastTab, "Ctrl+9")
	add(m.CmdMoveTabRight, "Ctrl+Shift+PageDown")
	add(m.CmdMoveTabLeft, "Ctrl+Shift+PageUp")
	add(m.CmdTabSearch, "Ctrl+Shift+A")
	add(m.CmdNewWindow, "Ctrl+N")
	add(m.CmdNewIncognito, "Ctrl+Shift+N")
	add(m.CmdCloseWindow, "Ctrl+Shift+W", "Alt+F4")
	add(m.CmdFullscreen, "F11")
	add(m.CmdBack, "Alt+Left")
	add(m.CmdForward, "Alt+Right")
	add(m.CmdReload, "Ctrl+R", "F5")
	add(m.CmdHardReload, "Ctrl+Shift+R", "Ctrl+F5", "Shift+F5")
	add(m.CmdHome, "Alt+Home")
	add(m.CmdFocusAddress, "Ctrl+L", "Alt+D")
	add(m.CmdFocusSearch, "Ctrl+E", "Ctrl+K")
	add(m.CmdFind, "Ctrl+F")
	add(m.CmdFindNext, "Ctrl+G", "F3")
	add(m.CmdFindPrevious, "Ctrl+Shift+G", "Shift+F3")
	add(m.CmdPrint, "Ctrl+P")
	add(m.CmdSystemPrint, "Ctrl+Shift+P")
	add(m.CmdSavePage, "Ctrl+S")
	add(m.CmdOpenFile, "Ctrl+O")
	add(m.CmdBookmark, "Ctrl+D")
	add(m.CmdBookmarkAll, "Ctrl+Shift+D")
	add(m.CmdZoomIn, "Ctrl+=", "Ctrl+Shift+=")
	add(m.CmdZoomOut, "Ctrl+-", "Ctrl+Shift+-")
	add(m.CmdZoomReset, "Ctrl+0")
	add(m.CmdViewSource, "Ctrl+U")
	add(m.CmdHistory, "Ctrl+H")
	add(m.CmdDownloads, "Ctrl+J")
	add(m.CmdBookmarks, "Ctrl+Shift+O")
	add(m.CmdBookmarkBar, "Ctrl+Shift+B")
	add(m.CmdClearData, "Ctrl+Shift+Delete")
	add(m.CmdTaskManager, "Shift+Esc")
	add(m.CmdDevTools, "F12", "Ctrl+Shift+I")
	add(m.CmdConsole, "Ctrl+Shift+J")
	add(m.CmdInspect, "Ctrl+Shift+C")
	add(m.CmdAppMenu, "Alt+E", "Alt+F")
	add(m.CmdHelp, "F1")
	add(m.CmdFocusPane, "F6", "Shift+F6", "Ctrl+F6")
	add(m.CmdCaret, "F7")
	add(m.CmdFocusMenu, "F10")
	add(m.CmdFocusBookmarks, "Alt+Shift+B")
	add(m.CmdFocusToolbar, "Alt+Shift+T")
	add(m.CmdProfileMenu, "Ctrl+Shift+M")
	return keys
}()

// ChromeSwitches are common Chromium switches. A value after "=" is an
// example for the user to change.
var ChromeSwitches = func() []Entry {
	type m = locales.Messages
	sw := func(value string, label Text) Entry { return Entry{Label: label, Value: value} }
	return slices.Concat(
		group(m.GroupStart,
			sw("--start-maximized", m.SwMaximized),
			sw("--start-fullscreen", m.SwFullscreen),
			sw("--window-size=1280,800", m.SwWindowSize),
			sw("--incognito", m.SwIncognito),
			sw("--no-first-run", m.SwNoFirstRun),
			sw("--no-default-browser-check", m.SwNoDefaultCheck),
		),
		group(m.GroupDisplay,
			sw("--force-dark-mode", m.SwDarkMode),
			sw("--force-device-scale-factor=1.25", m.SwScale),
			sw("--lang=zh-CN", m.SwLang),
		),
		group(m.GroupFeatures,
			sw("--disable-features=", m.SwDisableFeatures),
			sw("--enable-features=", m.SwEnableFeatures),
			sw("--disable-sync", m.SwNoSync),
			sw("--disable-component-update", m.SwNoComponents),
			sw("--disk-cache-size=1073741824", m.SwCacheSize),
		),
	)
}()
