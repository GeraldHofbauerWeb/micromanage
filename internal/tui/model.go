package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/GeraldHofbauerWeb/minecraft-instance-switcher/internal/instance"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type state int

const (
	stateList state = iota
	stateDetailPanel
	stateCreate
	stateConfirmDelete
	stateConfirmRestore
	stateConfirmFileDelete
	stateConfig     // NEW: show config variables list
	stateEditConfig // NEW: edit single config value
)

type detailPanel int

const (
	panelMods detailPanel = iota
	panelConfigs
	panelSaves
)

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Enter     key.Binding
	Back      key.Binding
	Quit      key.Binding
	Help      key.Binding
	Create    key.Binding
	Delete    key.Binding
	Refresh   key.Binding
	Restore   key.Binding
	Search    key.Binding
	TabNext   key.Binding
	TabPrev   key.Binding
	Edit      key.Binding
	Configure key.Binding // NEW: open config UI
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Create, k.Delete, k.Restore, k.Search, k.Configure, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter},
		{k.Create, k.Delete, k.Restore},
		{k.Search, k.Edit, k.Configure, k.Refresh, k.Help},
		{k.Back, k.Quit},
	}
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "move up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "move down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select/switch"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc", "backspace"),
		key.WithHelp("esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	// remapped from 'c' to 'a' (add instance)
	Create: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "add instance"),
	),
	Delete: key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete instance"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("f5"),
		key.WithHelp("f5", "refresh"),
	),
	Restore: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "restore default"),
	),
	Search: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "show details"),
	),
	TabNext: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next panel"),
	),
	TabPrev: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "prev panel"),
	),
	Edit: key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit config"),
	),
	Configure: key.NewBinding( // NEW
		key.WithKeys("c"),
		key.WithHelp("c", "configure"),
	),
}

// NEW: list item representing a config key/value
type configItem struct {
	Key   string
	Value string
}

func (c configItem) FilterValue() string { return c.Key + " " + c.Value }
func (c configItem) Title() string       { return c.Key }
func (c configItem) Description() string { return c.Value }

type instanceItem struct {
	instance.Instance
}

type searchItem struct {
	InstanceName string
	Directory    string
	Files        []string
	FileCount    int
}

type searchData struct {
	Instances map[string][]searchItem
	AllItems  []searchItem
}

func (i instanceItem) FilterValue() string { return i.Name }
func (i instanceItem) Title() string       { return i.Name }
func (i instanceItem) Description() string {
	var status string
	if i.IsActive {
		status = "● ACTIVE"
	} else {
		status = "○ Inactive"
	}
	return fmt.Sprintf("%s | %d mods | %d configs | %d saves",
		status, i.ModCount, i.ConfigCount, i.SaveCount)
}

func (s searchItem) FilterValue() string { return s.InstanceName + " " + s.Directory }
func (s searchItem) Title() string {
	return fmt.Sprintf("%s/%s", s.InstanceName, s.Directory)
}
func (s searchItem) Description() string {
	fileDesc := fmt.Sprintf("%d files", s.FileCount)
	if len(s.Files) > 0 {
		preview := strings.Join(s.Files[:min(len(s.Files), 3)], ", ")
		if len(s.Files) > 3 {
			preview += "..."
		}
		return fmt.Sprintf("%s: %s", fileDesc, preview)
	}
	return fileDesc
}

type fileItem struct {
	Name         string
	MaxWidth     int
	ScrollOffset int
	IsSelected   bool
}

func (f fileItem) FilterValue() string { return f.Name }

func (f fileItem) Title() string {
	// Safety check for MaxWidth
	if f.MaxWidth <= 0 {
		f.MaxWidth = 20 // Default fallback
	}

	// If selected and name is too long, scroll horizontally
	if f.IsSelected && len(f.Name) > f.MaxWidth {
		return f.scrollText(f.Name, f.MaxWidth, f.ScrollOffset)
	}
	// Otherwise truncate with ellipsis to prevent wrapping
	if len(f.Name) > f.MaxWidth {
		if f.MaxWidth <= 3 {
			return f.Name[:f.MaxWidth] // Very narrow, just cut off
		}
		return f.Name[:f.MaxWidth-3] + "..."
	}
	return f.Name
}

func (f fileItem) Description() string { return "" }

func (f fileItem) scrollText(text string, maxWidth, offset int) string {
	if len(text) <= maxWidth {
		return text
	}

	// Create a sliding window effect with minimal spacing
	visibleText := text + " " + text // Add minimal spacing and repeat
	start := offset % len(text)
	visiblePart := visibleText[start:]

	if len(visiblePart) >= maxWidth {
		return visiblePart[:maxWidth]
	}
	return visiblePart + visibleText[:maxWidth-len(visiblePart)]
}

type model struct {
	state            state
	manager          *instance.Manager
	list             list.Model
	searchList       list.Model
	modsList         list.Model
	configsList      list.Model
	savesList        list.Model
	help             help.Model
	textInput        textinput.Model
	instances        []instance.Instance
	selectedInstance *instance.Instance
	instanceInfo     *instance.InstanceInfo
	searchData       *searchData
	activePanel      detailPanel
	terminalWidth    int
	terminalHeight   int
	scrollOffset     int
	message          string
	err              error
	keys             keyMap
	// File deletion context
	fileToDelete string
	fileType     string // "mod", "config", or "save"

	// NEW: config UI
	configList list.Model
	editingKey string // the config key currently being edited
}

type refreshMsg struct{}
type switchMsg struct{ name string }
type createMsg struct{ name string }
type deleteMsg struct{ name string }
type restoreMsg struct{}
type confirmRestoreMsg struct{}
type tickMsg struct{}
type deleteFileMsg struct{ fileName, fileType string }

func initialModel() model {
	manager, err := instance.NewManager()

	// Initialize list
	items := []list.Item{}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Minecraft Instance Manager"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Styles.Title = titleStyle

	// Initialize search list
	searchItems := []list.Item{}
	sl := list.New(searchItems, list.NewDefaultDelegate(), 0, 0)
	sl.Title = "Instance Directory Search"
	sl.SetShowStatusBar(false)
	sl.SetFilteringEnabled(true)
	sl.Styles.Title = titleStyle

	// Initialize detail panel lists
	modsList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	modsList.Title = "Mods"
	modsList.SetShowStatusBar(false)
	modsList.SetFilteringEnabled(false)
	modsList.Styles.Title = titleStyle

	configsList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	configsList.Title = "Configs"
	configsList.SetShowStatusBar(false)
	configsList.SetFilteringEnabled(false)
	configsList.Styles.Title = titleStyle

	savesList := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	savesList.Title = "Saves"
	savesList.SetShowStatusBar(false)
	savesList.SetFilteringEnabled(false)
	savesList.Styles.Title = titleStyle

	// Initialize config list (NEW)
	cfgItems := []list.Item{}
	cfgList := list.New(cfgItems, list.NewDefaultDelegate(), 0, 0)
	cfgList.Title = "Configuration"
	cfgList.SetShowStatusBar(false)
	cfgList.SetFilteringEnabled(true)
	cfgList.Styles.Title = titleStyle

	// Initialize text input
	ti := textinput.New()
	ti.Placeholder = "Enter instance name..."
	ti.Focus()
	ti.CharLimit = 50
	ti.Width = 30

	// Initialize help
	h := help.New()

	m := model{
		state:          stateList,
		manager:        manager,
		list:           l,
		searchList:     sl,
		modsList:       modsList,
		configsList:    configsList,
		savesList:      savesList,
		help:           h,
		textInput:      ti,
		activePanel:    panelMods,
		terminalWidth:  120, // Default fallback
		terminalHeight: 30,  // Default fallback
		keys:           keys,
		err:            err,
		configList:     cfgList, // NEW
	}

	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		refreshInstances,
		textinput.Blink,
		tickCmd(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*300, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case stateList:
			return m.updateList(msg)
		case stateDetailPanel:
			return m.updateDetailPanel(msg)
		case stateCreate:
			return m.updateCreate(msg)
		case stateConfirmDelete:
			return m.updateConfirmDelete(msg)
		case stateConfirmRestore:
			return m.updateConfirmRestore(msg)
		case stateConfirmFileDelete:
			return m.updateConfirmFileDelete(msg)
		case stateConfig: // NEW
			return m.updateConfigList(msg)
		case stateEditConfig: // NEW
			return m.updateEditConfig(msg)
		}

	case tea.WindowSizeMsg:
		// Safety check for minimum terminal size
		if msg.Width < 10 || msg.Height < 10 {
			return m, nil
		}

		// Store terminal dimensions
		m.terminalWidth = msg.Width
		m.terminalHeight = msg.Height

		m.list.SetSize(msg.Width, msg.Height-4)
		m.searchList.SetSize(msg.Width, msg.Height-4)
		m.configList.SetSize(msg.Width, msg.Height-4) // NEW: set size for config list

		// Update text input width to match terminal width (with some padding)
		textInputWidth := msg.Width - 4 // Leave 4 chars for padding/borders
		if textInputWidth < 20 {
			textInputWidth = 20 // Minimum usable width
		}
		m.textInput.Width = textInputWidth

		// Set panel sizes for detail view (each panel gets 1/3 of width)
		// Account for borders (2 chars) + padding (2 chars) per panel, but be less conservative
		panelWidth := (msg.Width / 3) - 2 // Minimal border accounting
		if panelWidth < 15 {
			panelWidth = 15 // Minimum usable width
		}
		panelHeight := msg.Height - 6 // Leave space for header and instructions
		if panelHeight < 5 {
			panelHeight = 5 // Minimum height
		}
		m.modsList.SetSize(panelWidth, panelHeight)
		m.configsList.SetSize(panelWidth, panelHeight)
		m.savesList.SetSize(panelWidth, panelHeight)
		return m, nil

	case refreshMsg:
		instances, err := m.manager.ListInstances()
		if err != nil {
			m.err = err
			return m, nil
		}

		m.instances = instances
		items := make([]list.Item, len(instances))
		for i, inst := range instances {
			items[i] = instanceItem{inst}
		}

		m.list.SetItems(items)
		m.err = nil
		return m, nil

	case switchMsg:
		err := m.manager.SwitchInstance(msg.name)
		if err != nil {
			m.err = err
		} else {
			m.message = fmt.Sprintf("Switched to instance: %s", msg.name)
		}
		return m, refreshInstances

	case createMsg:
		err := m.manager.CreateInstance(msg.name)
		if err != nil {
			m.err = err
		} else {
			m.message = fmt.Sprintf("Created instance: %s", msg.name)
			m.state = stateList
			// Clear the text input and unfocus it
			m.textInput.SetValue("")
			m.textInput.Blur()
		}
		return m, refreshInstances

	case deleteMsg:
		err := m.manager.DeleteInstance(msg.name)
		if err != nil {
			m.err = err
		} else {
			m.message = fmt.Sprintf("Deleted instance: %s", msg.name)
			m.state = stateList
		}
		return m, refreshInstances

	case restoreMsg:
		err := m.manager.RestoreDefault()
		if err != nil {
			m.err = err
		} else {
			m.message = "Restored default minecraft directory"
		}
		return m, refreshInstances

	case deleteFileMsg:
		err := m.deleteFile(msg.fileName, msg.fileType)
		if err != nil {
			m.err = err
		} else {
			m.message = fmt.Sprintf("Deleted %s: %s", msg.fileType, msg.fileName)
			// Refresh the instance info to update the lists
			if info, infoErr := m.manager.GetInstanceInfo(m.selectedInstance.Name); infoErr == nil {
				m.instanceInfo = info
				m.refreshDetailPanelLists()
			}
		}
		m.state = stateDetailPanel
		return m, nil

	// searchMsg removed - we now go directly to 3-column panel view

	case confirmRestoreMsg:
		m.state = stateConfirmRestore
		return m, nil

	case tickMsg:
		// Update scroll offset for filename scrolling, but only in detail panel view
		if m.state == stateDetailPanel {
			m.scrollOffset++
			// Update items in the currently focused list with new scroll offset and selection state
			switch m.activePanel {
			case panelMods:
				items := m.modsList.Items()
				for i, item := range items {
					if fileItem, ok := item.(fileItem); ok {
						fileItem.ScrollOffset = m.scrollOffset
						fileItem.IsSelected = (i == m.modsList.Index())
						items[i] = fileItem
					}
				}
				m.modsList.SetItems(items)
			case panelConfigs:
				items := m.configsList.Items()
				for i, item := range items {
					if fileItem, ok := item.(fileItem); ok {
						fileItem.ScrollOffset = m.scrollOffset
						fileItem.IsSelected = (i == m.configsList.Index())
						items[i] = fileItem
					}
				}
				m.configsList.SetItems(items)
			case panelSaves:
				items := m.savesList.Items()
				for i, item := range items {
					if fileItem, ok := item.(fileItem); ok {
						fileItem.ScrollOffset = m.scrollOffset
						fileItem.IsSelected = (i == m.savesList.Index())
						items[i] = fileItem
					}
				}
				m.savesList.SetItems(items)
			}
		}
		return m, tickCmd()
	}

	// Update sub-models
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	m.searchList, cmd = m.searchList.Update(msg)
	cmds = append(cmds, cmd)

	m.modsList, cmd = m.modsList.Update(msg)
	cmds = append(cmds, cmd)

	m.configsList, cmd = m.configsList.Update(msg)
	cmds = append(cmds, cmd)

	m.savesList, cmd = m.savesList.Update(msg)
	cmds = append(cmds, cmd)

	// NEW: update config list sub-model (only active in config states)
	m.configList, cmd = m.configList.Update(msg)
	cmds = append(cmds, cmd)

	m.textInput, cmd = m.textInput.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Enter):
		if len(m.instances) == 0 {
			return m, nil
		}

		selected := m.list.SelectedItem().(instanceItem)
		if selected.IsActive {
			// Show 3-column detail view if already active
			m.selectedInstance = &selected.Instance
			info, err := m.manager.GetInstanceInfo(selected.Name)
			if err != nil {
				m.err = err
				return m, nil
			}
			m.instanceInfo = info

			// Populate the detail panel lists directly
			// Account for borders (2 chars) + padding (2 chars) per panel, but be less conservative
			panelWidth := (m.terminalWidth / 3) - 2 // Minimal border accounting
			if panelWidth < 15 {
				panelWidth = 15 // Minimum usable width
			}
			// Account for list item padding and borders for actual text width
			itemMaxWidth := panelWidth - 4 // Account for border (2) and list padding (2)

			modsItems := make([]list.Item, len(info.ModsDir))
			for i, mod := range info.ModsDir {
				modsItems[i] = fileItem{
					Name:         mod,
					MaxWidth:     itemMaxWidth,
					ScrollOffset: m.scrollOffset,
					IsSelected:   i == 0, // First item is selected initially
				}
			}
			m.modsList.SetItems(modsItems)

			configsItems := make([]list.Item, len(info.ConfigsDir))
			for i, config := range info.ConfigsDir {
				configsItems[i] = fileItem{
					Name:         config,
					MaxWidth:     itemMaxWidth,
					ScrollOffset: m.scrollOffset,
					IsSelected:   false, // Not initially selected
				}
			}
			m.configsList.SetItems(configsItems)

			savesItems := make([]list.Item, len(info.SavesDir))
			for i, save := range info.SavesDir {
				savesItems[i] = fileItem{
					Name:         save,
					MaxWidth:     itemMaxWidth,
					ScrollOffset: m.scrollOffset,
					IsSelected:   false, // Not initially selected
				}
			}
			m.savesList.SetItems(savesItems)

			m.activePanel = panelMods
			m.state = stateDetailPanel
		} else {
			// Switch to this instance
			return m, func() tea.Msg {
				return switchMsg{name: selected.Name}
			}
		}

	case key.Matches(msg, m.keys.Create):
		m.state = stateCreate
		m.textInput.SetValue("")
		m.textInput.Focus()

	case key.Matches(msg, m.keys.Delete):
		if len(m.instances) == 0 {
			return m, nil
		}

		selected := m.list.SelectedItem().(instanceItem)
		if selected.IsActive {
			m.err = fmt.Errorf("cannot delete active instance")
			return m, nil
		}

		m.selectedInstance = &selected.Instance
		m.state = stateConfirmDelete

	case key.Matches(msg, m.keys.Refresh):
		m.message = ""
		m.err = nil
		return m, refreshInstances

	case key.Matches(msg, m.keys.Restore):
		return m, func() tea.Msg {
			return confirmRestoreMsg{}
		}

	case key.Matches(msg, m.keys.Search):
		if len(m.instances) == 0 {
			return m, nil
		}

		selected := m.list.SelectedItem().(instanceItem)
		// Show 3-column detail view for selected instance
		m.selectedInstance = &selected.Instance
		info, err := m.manager.GetInstanceInfo(selected.Name)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.instanceInfo = info

		// Calculate proper width for file items
		panelWidth := (m.terminalWidth / 3) - 2 // Minimal border accounting
		if panelWidth < 15 {
			panelWidth = 15 // Minimum usable width
		}
		// Account for list item padding and borders for actual text width
		itemMaxWidth := panelWidth - 4 // Account for border (2) and list padding (2)

		// Populate the detail panel lists
		modsItems := make([]list.Item, len(info.ModsDir))
		for i, mod := range info.ModsDir {
			modsItems[i] = fileItem{
				Name:     mod,
				MaxWidth: itemMaxWidth,
			}
		}
		m.modsList.SetItems(modsItems)

		configsItems := make([]list.Item, len(info.ConfigsDir))
		for i, config := range info.ConfigsDir {
			configsItems[i] = fileItem{
				Name:     config,
				MaxWidth: itemMaxWidth,
			}
		}
		m.configsList.SetItems(configsItems)

		savesItems := make([]list.Item, len(info.SavesDir))
		for i, save := range info.SavesDir {
			savesItems[i] = fileItem{
				Name:     save,
				MaxWidth: itemMaxWidth,
			}
		}
		m.savesList.SetItems(savesItems)

		m.activePanel = panelMods
		m.state = stateDetailPanel

	case key.Matches(msg, m.keys.Configure): // NEW: open global configuration UI
		var items []list.Item
		if m.manager != nil {
			items = m.buildConfigListItems()
		} else {
			// Fallback if manager is not available
			items = []list.Item{
				configItem{Key: "Error", Value: "Manager not initialized"},
			}
		}
		m.configList.SetItems(items)
		m.configList.Title = "Configuration"
		m.configList.SetFilteringEnabled(true)
		m.textInput.Blur()
		m.state = stateConfig
		return m, nil

	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Quit):
		m.state = stateList
	case key.Matches(msg, m.keys.Search): // 's' key to show detailed panels
		// Populate the detail panel lists
		if m.instanceInfo != nil {
			// Populate mods list
			modsItems := make([]list.Item, len(m.instanceInfo.ModsDir))
			for i, mod := range m.instanceInfo.ModsDir {
				modsItems[i] = fileItem{Name: mod}
			}
			m.modsList.SetItems(modsItems)

			// Populate configs list
			configsItems := make([]list.Item, len(m.instanceInfo.ConfigsDir))
			for i, config := range m.instanceInfo.ConfigsDir {
				configsItems[i] = fileItem{Name: config}
			}
			m.configsList.SetItems(configsItems)

			// Populate saves list
			savesItems := make([]list.Item, len(m.instanceInfo.SavesDir))
			for i, save := range m.instanceInfo.SavesDir {
				savesItems[i] = fileItem{Name: save}
			}
			m.savesList.SetItems(savesItems)

			m.activePanel = panelMods
			m.state = stateDetailPanel
		}
	}
	return m, nil
}

func (m model) updateCreate(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Enter):
		name := strings.TrimSpace(m.textInput.Value())
		if name != "" {
			return m, func() tea.Msg {
				return createMsg{name: name}
			}
		}

	case key.Matches(msg, m.keys.Back):
		m.state = stateList
		m.textInput.Blur()
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateConfirmDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, func() tea.Msg {
			return deleteMsg{name: m.selectedInstance.Name}
		}
	case "n", "N", "esc":
		m.state = stateList
	}
	return m, nil
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Quit):
		m.state = stateList
	}

	// Update search list
	var cmd tea.Cmd
	m.searchList, cmd = m.searchList.Update(msg)
	return m, cmd
}

func (m model) updateDetailPanel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back), key.Matches(msg, m.keys.Quit):
		m.state = stateList
	case key.Matches(msg, m.keys.TabNext):
		m.activePanel = (m.activePanel + 1) % 3
	case key.Matches(msg, m.keys.TabPrev):
		m.activePanel = (m.activePanel + 2) % 3 // +2 is same as -1 in mod 3
	case key.Matches(msg, m.keys.Delete):
		// Allow deleting files from any panel
		if m.selectedInstance != nil {
			var fileName string
			var fileType string

			switch m.activePanel {
			case panelMods:
				if m.modsList.SelectedItem() != nil {
					fileName = m.modsList.SelectedItem().(fileItem).Name
					fileType = "mod"
				}
			case panelConfigs:
				if m.configsList.SelectedItem() != nil {
					fileName = m.configsList.SelectedItem().(fileItem).Name
					fileType = "config"
				}
			case panelSaves:
				if m.savesList.SelectedItem() != nil {
					fileName = m.savesList.SelectedItem().(fileItem).Name
					fileType = "save"
				}
			}

			if fileName != "" {
				m.fileToDelete = fileName
				m.fileType = fileType
				m.state = stateConfirmFileDelete
			}
		}
	case key.Matches(msg, m.keys.Edit):
		// Only allow editing in config panel
		if m.activePanel == panelConfigs && m.selectedInstance != nil {
			if m.configsList.SelectedItem() != nil {
				configFile := m.configsList.SelectedItem().(fileItem)
				return m, m.editConfigFile(configFile.Name)
			}
		}
	case key.Matches(msg, m.keys.Configure): // NEW
		// Open config UI for the selected instance
		if m.selectedInstance != nil {
			// show global config UI (paths are global) - reuse same config list
			m.buildConfigListItems()
			m.configList.SetFilteringEnabled(true)
			m.state = stateConfig
		}
	}

	// Update the appropriate list based on active panel
	var cmd tea.Cmd
	switch m.activePanel {
	case panelMods:
		m.modsList, cmd = m.modsList.Update(msg)
		// Update selection state for all items
		m.updateItemSelectionState(panelMods)
	case panelConfigs:
		m.configsList, cmd = m.configsList.Update(msg)
		// Update selection state for all items
		m.updateItemSelectionState(panelConfigs)
	case panelSaves:
		m.savesList, cmd = m.savesList.Update(msg)
		// Update selection state for all items
		m.updateItemSelectionState(panelSaves)
	}
	return m, cmd
}

func (m model) updateConfirmRestore(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r", "enter":
		return m, func() tea.Msg {
			return restoreMsg{}
		}
	case "esc":
		m.state = stateList
	}
	return m, nil
}

// updateItemSelectionState updates the IsSelected field for all items in the specified panel
func (m *model) updateItemSelectionState(panel detailPanel) {
	switch panel {
	case panelMods:
		items := m.modsList.Items()
		for i, item := range items {
			if fileItem, ok := item.(fileItem); ok {
				fileItem.IsSelected = (i == m.modsList.Index())
				items[i] = fileItem
			}
		}
		m.modsList.SetItems(items)
	case panelConfigs:
		items := m.configsList.Items()
		for i, item := range items {
			if fileItem, ok := item.(fileItem); ok {
				fileItem.IsSelected = (i == m.configsList.Index())
				items[i] = fileItem
			}
		}
		m.configsList.SetItems(items)
	case panelSaves:
		items := m.savesList.Items()
		for i, item := range items {
			if fileItem, ok := item.(fileItem); ok {
				fileItem.IsSelected = (i == m.savesList.Index())
				items[i] = fileItem
			}
		}
		m.savesList.SetItems(items)
	}
}

func (m model) View() string {
	switch m.state {
	case stateList:
		return m.viewList()
	case stateDetailPanel:
		return m.viewDetailPanel()
	case stateCreate:
		return m.viewCreate()
	case stateConfirmDelete:
		return m.viewConfirmDelete()
	case stateConfirmRestore:
		return m.viewConfirmRestore()
	case stateConfirmFileDelete:
		return m.viewConfirmFileDelete()
	case stateConfig: // NEW
		return m.viewConfig()
	case stateEditConfig: // NEW
		return m.viewEditConfig()
	}
	return ""
}

func (m model) viewList() string {
	var content strings.Builder

	if m.err != nil {
		content.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		content.WriteString("\n\n")
	}

	if m.message != "" {
		content.WriteString(successStyle.Render(m.message))
		content.WriteString("\n\n")
	}

	content.WriteString(m.list.View())
	content.WriteString("\n")
	content.WriteString(m.help.View(m.keys))

	return content.String()
}

func (m model) viewDetail() string {
	if m.selectedInstance == nil || m.instanceInfo == nil {
		return "No instance selected"
	}

	var content strings.Builder

	content.WriteString(titleStyle.Render(fmt.Sprintf("Instance Details: %s", m.selectedInstance.Name)))
	content.WriteString("\n\n")

	// Instance stats
	content.WriteString(subtitleStyle.Render("Statistics:"))
	content.WriteString("\n")
	content.WriteString(fmt.Sprintf("• Mods: %d\n", m.selectedInstance.ModCount))
	content.WriteString(fmt.Sprintf("• Configs: %d\n", m.selectedInstance.ConfigCount))
	content.WriteString(fmt.Sprintf("• Saves: %d\n", m.selectedInstance.SaveCount))
	content.WriteString(fmt.Sprintf("• Status: %s\n", func() string {
		if m.selectedInstance.IsActive {
			return "Active"
		}
		return "Inactive"
	}()))
	content.WriteString("\n")

	// Mods list
	if len(m.instanceInfo.ModsDir) > 0 {
		content.WriteString(subtitleStyle.Render("Mods:"))
		content.WriteString("\n")
		for _, mod := range m.instanceInfo.ModsDir[:min(len(m.instanceInfo.ModsDir), 10)] {
			content.WriteString(fmt.Sprintf("• %s\n", mod))
		}
		if len(m.instanceInfo.ModsDir) > 10 {
			content.WriteString(fmt.Sprintf("... and %d more\n", len(m.instanceInfo.ModsDir)-10))
		}
		content.WriteString("\n")
	}

	// Saves list
	if len(m.instanceInfo.SavesDir) > 0 {
		content.WriteString(subtitleStyle.Render("Saves:"))
		content.WriteString("\n")
		for _, save := range m.instanceInfo.SavesDir {
			content.WriteString(fmt.Sprintf("• %s\n", save))
		}
		content.WriteString("\n")
	}

	content.WriteString(dimStyle.Render("Press ESC to go back"))

	return content.String()
}

func (m model) viewCreate() string {
	var content strings.Builder

	content.WriteString(titleStyle.Render("Create New Instance"))
	content.WriteString("\n\n")
	content.WriteString("Instance name:\n")
	content.WriteString(m.textInput.View())
	content.WriteString("\n\n")
	content.WriteString(dimStyle.Render("Press Enter to create, ESC to cancel"))

	return content.String()
}

func (m model) viewConfirmDelete() string {
	if m.selectedInstance == nil {
		return ""
	}

	var content strings.Builder

	content.WriteString(titleStyle.Render("Confirm Deletion"))
	content.WriteString("\n\n")
	content.WriteString(fmt.Sprintf("Are you sure you want to delete instance '%s'?\n", m.selectedInstance.Name))
	content.WriteString("This action cannot be undone.\n\n")
	content.WriteString(errorStyle.Render("Press 'y' to confirm, 'n' to cancel"))

	return content.String()
}

func (m model) viewSearch() string {
	var content strings.Builder

	if m.err != nil {
		content.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		content.WriteString("\n\n")
	}

	content.WriteString(m.searchList.View())
	content.WriteString("\n")
	content.WriteString(dimStyle.Render("Press ESC to go back, / to filter, Enter to view details"))

	return content.String()
}

func (m model) viewConfirmRestore() string {
	var content strings.Builder

	content.WriteString(titleStyle.Render("Confirm Restore Default"))
	content.WriteString("\n\n")
	content.WriteString("⚠️  Are you sure you want to restore the default .minecraft directory?\n\n")
	content.WriteString("This will:\n")
	content.WriteString("• Remove the current instance symlink\n")
	content.WriteString("• Restore your original .minecraft folder from backup\n")
	content.WriteString("• Switch back to your pre-instance-manager setup\n\n")
	content.WriteString(successStyle.Render("Press 'r' or Enter to restore"))
	content.WriteString("  ")
	content.WriteString(dimStyle.Render("Press ESC to cancel"))

	return content.String()
}

func (m model) viewDetailPanel() string {
	if m.selectedInstance == nil {
		return "No instance selected"
	}

	// Use stored terminal width or fallback
	terminalWidth := m.terminalWidth
	if terminalWidth == 0 {
		terminalWidth = 120 // Default fallback
	}
	// Account for borders (2 chars) + padding (2 chars) per panel, but be less conservative
	panelWidth := (terminalWidth / 3) - 2 // Minimal border accounting
	if panelWidth < 15 {
		panelWidth = 15 // Minimum usable width
	}

	// Apply active styles to titles
	if m.activePanel == panelMods {
		m.modsList.Styles.Title = titleStyle
		m.configsList.Styles.Title = dimStyle
		m.savesList.Styles.Title = dimStyle
	} else if m.activePanel == panelConfigs {
		m.modsList.Styles.Title = dimStyle
		m.configsList.Styles.Title = titleStyle
		m.savesList.Styles.Title = dimStyle
	} else {
		m.modsList.Styles.Title = dimStyle
		m.configsList.Styles.Title = dimStyle
		m.savesList.Styles.Title = titleStyle
	}

	// Create header
	header := titleStyle.Render(fmt.Sprintf("Instance Details: %s", m.selectedInstance.Name))

	// Create panel styles with borders and minimal padding
	activePanelStyle := lipgloss.NewStyle().
		Width(panelWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(0, 0)

	inactivePanelStyle := lipgloss.NewStyle().
		Width(panelWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#444444")).
		Padding(0, 0)

	// Apply appropriate styles based on active panel
	var modsView, configsView, savesView string
	if m.activePanel == panelMods {
		modsView = activePanelStyle.Render(m.modsList.View())
		configsView = inactivePanelStyle.Render(m.configsList.View())
		savesView = inactivePanelStyle.Render(m.savesList.View())
	} else if m.activePanel == panelConfigs {
		modsView = inactivePanelStyle.Render(m.modsList.View())
		configsView = activePanelStyle.Render(m.configsList.View())
		savesView = inactivePanelStyle.Render(m.savesList.View())
	} else {
		modsView = inactivePanelStyle.Render(m.modsList.View())
		configsView = inactivePanelStyle.Render(m.configsList.View())
		savesView = activePanelStyle.Render(m.savesList.View())
	}

	// Join the three panels horizontally
	panelsView := lipgloss.JoinHorizontal(
		lipgloss.Top,
		modsView,
		configsView,
		savesView,
	)

	// Instructions
	instructions := dimStyle.Render("Tab/Shift+Tab to switch panels • 'd' to delete file • 'e' to edit config • ESC to go back • ↑/↓ to navigate")

	return fmt.Sprintf("%s\n\n%s\n\n%s", header, panelsView, instructions)
}

func refreshInstances() tea.Msg {
	return refreshMsg{}
}

func (m model) editConfigFile(configFileName string) tea.Cmd {
	if m.selectedInstance == nil {
		return nil
	}

	return tea.ExecProcess(&exec.Cmd{
		Path: getEditor(),
		Args: []string{
			getEditor(),
			filepath.Join(m.manager.InstancesPath, m.selectedInstance.Name, "config", configFileName),
		},
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}, nil)
}

func getEditor() string {
	// Try to get editor from environment variables
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}

	// Fallback to common editors in order of preference
	editors := []string{"nano", "vim", "vi", "emacs", "code", "gedit"}
	for _, editor := range editors {
		if _, err := exec.LookPath(editor); err == nil {
			return editor
		}
	}

	// Ultimate fallback
	return "vi"
}

func (m *model) buildSearchData() (*searchData, error) {
	instances, err := m.manager.ListInstances()
	if err != nil {
		return nil, err
	}

	searchData := &searchData{
		Instances: make(map[string][]searchItem),
		AllItems:  []searchItem{},
	}

	for _, inst := range instances {
		instanceItems := []searchItem{}

		// Get detailed instance info
		info, err := m.manager.GetInstanceInfo(inst.Name)
		if err != nil {
			continue // Skip this instance if we can't read it
		}

		// Add mods directory
		if len(info.ModsDir) > 0 {
			item := searchItem{
				InstanceName: inst.Name,
				Directory:    "mods",
				Files:        info.ModsDir,
				FileCount:    len(info.ModsDir),
			}
			instanceItems = append(instanceItems, item)
			searchData.AllItems = append(searchData.AllItems, item)
		}

		// Add config directory
		if len(info.ConfigsDir) > 0 {
			item := searchItem{
				InstanceName: inst.Name,
				Directory:    "config",
				Files:        info.ConfigsDir[:min(len(info.ConfigsDir), 20)], // Limit for display
				FileCount:    len(info.ConfigsDir),
			}
			instanceItems = append(instanceItems, item)
			searchData.AllItems = append(searchData.AllItems, item)
		}

		// Add saves directory
		if len(info.SavesDir) > 0 {
			item := searchItem{
				InstanceName: inst.Name,
				Directory:    "saves",
				Files:        info.SavesDir,
				FileCount:    len(info.SavesDir),
			}
			instanceItems = append(instanceItems, item)
			searchData.AllItems = append(searchData.AllItems, item)
		}

		searchData.Instances[inst.Name] = instanceItems
	}

	return searchData, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// deleteFile deletes a file from the appropriate directory
func (m model) deleteFile(fileName, fileType string) error {
	if m.selectedInstance == nil {
		return fmt.Errorf("no instance selected")
	}

	var filePath string
	instancePath := filepath.Join(m.manager.InstancesPath, m.selectedInstance.Name)

	switch fileType {
	case "mod":
		filePath = filepath.Join(instancePath, "mods", fileName)
	case "config":
		filePath = filepath.Join(instancePath, "config", fileName)
	case "save":
		filePath = filepath.Join(instancePath, "saves", fileName)
	default:
		return fmt.Errorf("unknown file type: %s", fileType)
	}

	// Check if file/directory exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("%s does not exist", fileName)
	}

	// Delete the file or directory
	return os.RemoveAll(filePath)
}

// refreshDetailPanelLists refreshes all detail panel lists with current instance info
func (m *model) refreshDetailPanelLists() {
	if m.instanceInfo == nil {
		return
	}

	// Calculate proper width for file items
	panelWidth := (m.terminalWidth / 3) - 2
	if panelWidth < 15 {
		panelWidth = 15
	}
	itemMaxWidth := panelWidth - 4

	// Refresh mods list
	modsItems := make([]list.Item, len(m.instanceInfo.ModsDir))
	for i, mod := range m.instanceInfo.ModsDir {
		modsItems[i] = fileItem{
			Name:         mod,
			MaxWidth:     itemMaxWidth,
			ScrollOffset: m.scrollOffset,
			IsSelected:   false,
		}
	}
	m.modsList.SetItems(modsItems)

	// Refresh configs list
	configsItems := make([]list.Item, len(m.instanceInfo.ConfigsDir))
	for i, config := range m.instanceInfo.ConfigsDir {
		configsItems[i] = fileItem{
			Name:         config,
			MaxWidth:     itemMaxWidth,
			ScrollOffset: m.scrollOffset,
			IsSelected:   false,
		}
	}
	m.configsList.SetItems(configsItems)

	// Refresh saves list
	savesItems := make([]list.Item, len(m.instanceInfo.SavesDir))
	for i, save := range m.instanceInfo.SavesDir {
		savesItems[i] = fileItem{
			Name:         save,
			MaxWidth:     itemMaxWidth,
			ScrollOffset: m.scrollOffset,
			IsSelected:   false,
		}
	}
	m.savesList.SetItems(savesItems)
}

// updateConfirmFileDelete handles file deletion confirmation
func (m model) updateConfirmFileDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, func() tea.Msg {
			return deleteFileMsg{fileName: m.fileToDelete, fileType: m.fileType}
		}
	case "n", "N", "esc":
		m.state = stateDetailPanel
	}
	return m, nil
}

// viewConfirmFileDelete shows the file deletion confirmation dialog
func (m model) viewConfirmFileDelete() string {
	var content strings.Builder

	content.WriteString(titleStyle.Render("Confirm File Deletion"))
	content.WriteString("\n\n")
	content.WriteString(fmt.Sprintf("Are you sure you want to delete this %s?\n", m.fileType))
	content.WriteString(fmt.Sprintf("File: %s\n", m.fileToDelete))
	content.WriteString("This action cannot be undone.\n\n")
	content.WriteString(errorStyle.Render("Press 'y' to confirm, 'n' or ESC to cancel"))

	return content.String()
}

// NEW: update handler for config list view
func (m model) updateConfigList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Back):
		m.state = stateList
		return m, nil
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Enter):
		// Open selected config for editing
		if m.configList.SelectedItem() == nil {
			return m, nil
		}
		selected := m.configList.SelectedItem().(configItem)
		m.editingKey = selected.Key
		// prepare text input for editing
		m.textInput.SetValue(selected.Value)
		m.textInput.Placeholder = "Enter new value..."
		m.textInput.CursorEnd()
		m.textInput.Focus()
		m.state = stateEditConfig
		return m, nil
	}

	// update the config list sub-model
	var cmd tea.Cmd
	m.configList, cmd = m.configList.Update(msg)
	return m, cmd
}

// NEW: handler for editing a single config value
func (m model) updateEditConfig(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Enter):
		newVal := strings.TrimSpace(m.textInput.Value())
		// attempt update
		if err := m.manager.UpdateConfig(m.editingKey, newVal); err != nil {
			m.err = err
			return m, nil
		}
		// success: refresh config list items and go back to config list
		m.message = fmt.Sprintf("Updated %s -> %s", m.editingKey, newVal)
		items := m.buildConfigListItems()
		m.configList.SetItems(items)
		m.configList.Title = "Configuration"
		m.textInput.Blur()
		m.state = stateConfig
		return m, nil
	case key.Matches(msg, m.keys.Back):
		// cancel editing
		m.textInput.Blur()
		m.state = stateConfig
		return m, nil
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	}

	// update text input
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// NEW: build config list items from current manager config
func (m model) buildConfigListItems() []list.Item {
	if m.manager == nil {
		return []list.Item{
			configItem{Key: "Error", Value: "Manager not initialized"},
		}
	}

	cfg := m.manager.GetConfig()
	if len(cfg) == 0 {
		return []list.Item{
			configItem{Key: "Error", Value: "No configuration found"},
		}
	}

	items := make([]list.Item, 0, len(cfg))
	// deterministic order: minecraft-path, instances-path, backup-path, app-dir, config-file
	keysOrder := []string{"minecraft-path", "instances-path", "backup-path", "app-dir", "config-file"}
	for _, k := range keysOrder {
		if v, ok := cfg[k]; ok {
			items = append(items, configItem{Key: k, Value: v})
		}
	}
	// include any other keys not in the manual order
	for k, v := range cfg {
		found := false
		for _, kk := range keysOrder {
			if kk == k {
				found = true
				break
			}
		}
		if !found {
			items = append(items, configItem{Key: k, Value: v})
		}
	}

	if len(items) == 0 {
		return []list.Item{
			configItem{Key: "Error", Value: "No config items found"},
		}
	}

	return items
}

// NEW: view functions for config states
func (m model) viewConfig() string {
	var content strings.Builder

	if m.err != nil {
		content.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		content.WriteString("\n\n")
	}
	if m.message != "" {
		content.WriteString(successStyle.Render(m.message))
		content.WriteString("\n\n")
	}

	content.WriteString(m.configList.View())
	content.WriteString("\n\n")
	content.WriteString(dimStyle.Render("Enter to edit • ESC to go back • / to filter • q to quit"))
	return content.String()
}

func (m model) viewEditConfig() string {
	var content strings.Builder
	content.WriteString(titleStyle.Render(fmt.Sprintf("Edit Config: %s", m.editingKey)))
	content.WriteString("\n\n")
	content.WriteString("Value:\n")
	content.WriteString(m.textInput.View())
	content.WriteString("\n\n")
	content.WriteString(dimStyle.Render("Enter to save • ESC to cancel"))
	return content.String()
}

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			Bold(true)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00")).
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))
)
