package gui

import (
	"gioui.org/widget"
	"golang.org/x/exp/shiny/materialdesign/icons"
)

// iconSet holds the glyphs the interface uses, parsed once.
type iconSet struct {
	Refresh, Settings, Account, Play, Stop, Add, Search,
	Eye, EyeOff, More, Close, Back, OpenInNew, Folder, Delete,
	Edit, Launch, Info, Warning, Power, Clear, SignOut, Photo,
	Bug, Save, Build, Memory, Check, Description, DropDown, Download *widget.Icon
}

func loadIcons() iconSet {
	mk := func(data []byte) *widget.Icon {
		ic, err := widget.NewIcon(data)
		if err != nil {
			panic("gui: bad icon data: " + err.Error())
		}
		return ic
	}
	return iconSet{
		Refresh:     mk(icons.NavigationRefresh),
		Settings:    mk(icons.ActionSettings),
		Account:     mk(icons.ActionAccountCircle),
		Play:        mk(icons.AVPlayArrow),
		Stop:        mk(icons.AVStop),
		Add:         mk(icons.ContentAdd),
		Search:      mk(icons.ActionSearch),
		Eye:         mk(icons.ActionVisibility),
		EyeOff:      mk(icons.ActionVisibilityOff),
		More:        mk(icons.NavigationMoreHoriz),
		Close:       mk(icons.NavigationClose),
		Back:        mk(icons.NavigationArrowBack),
		OpenInNew:   mk(icons.ActionOpenInNew),
		Folder:      mk(icons.FileFolderOpen),
		Delete:      mk(icons.ActionDelete),
		Edit:        mk(icons.EditorModeEdit),
		Launch:      mk(icons.ActionLaunch),
		Info:        mk(icons.ActionInfoOutline),
		Warning:     mk(icons.AlertWarning),
		Power:       mk(icons.ActionPowerSettingsNew),
		Clear:       mk(icons.ContentClear),
		SignOut:     mk(icons.ActionExitToApp),
		Photo:       mk(icons.ImagePhoto),
		Bug:         mk(icons.ActionBugReport),
		Save:        mk(icons.ContentSave),
		Build:       mk(icons.ActionBuild),
		Memory:      mk(icons.HardwareMemory),
		Check:       mk(icons.NavigationCheck),
		Description: mk(icons.ActionDescription),
		DropDown:    mk(icons.NavigationArrowDropDown),
		Download:    mk(icons.FileFileDownload),
	}
}
