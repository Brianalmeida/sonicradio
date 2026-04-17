package ui

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/dancnb/sonicradio/browser"
	"github.com/dancnb/sonicradio/config"
	smodel "github.com/dancnb/sonicradio/model"
	"github.com/dancnb/sonicradio/player"
	"github.com/dancnb/sonicradio/player/metadata"
)

const (
	// view messages, nweline is important to sync with list no items view
	loadingMsg          = "\n  Fetching stations... \n"
	noFavoritesAddedMsg = "\n  No favorite stations added.\n"
	noStationsFound     = "\n  No stations found. \n"
	emptyHistoryMsg     = "\n  No playback history available. \n"

	// header status
	noPlayingMsg     = "Nothing playing"
	missingFavorites = "Some stations were not found"
	prevTermErr      = "Could not terminate previous playback!"
	voteSuccesful    = "Station was voted successfully"
	statusMsgTimeout = 1 * time.Second

	// metadata
	volumeFmt          = "%3d%%%s"
	playerPollInterval = 500 * time.Millisecond
)

func NewModel(ctx context.Context, cfg *config.Value, b *browser.API, p *player.Player) *Model {
	m := newModel(ctx, cfg, b, p)
	progr := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
	m.Progr = progr
	trapSignal(progr)
	go updatePlayerMetadata(ctx, progr, m)
	return m
}

func newModel(ctx context.Context, cfg *config.Value, b *browser.API, p *player.Player) *Model {
	style := NewStyle(cfg.Theme)

	delegate := newStationDelegate(cfg, style, p, b)

	infoModel := newInfoModel(b, style)
	m := Model{
		cfg:          cfg,
		style:        style,
		browser:      b,
		player:       p,
		delegate:     delegate,
		statusUpdate: make(chan struct{}),

		volumeBar: getVolumeBar(style.GetSecondColor()),
	}
	m.tabs = []uiTab{
		newFavoritesTab(cfg, infoModel, b, style),
		newBrowseTab(ctx, b, infoModel, style),
		newHistoryTab(ctx, cfg, style),
		newSettingsTab(ctx, cfg, style, p.AvailablePlayerTypes(), m.changeTheme),
	}
	m.nowPlaying = newNowPlayingModel(&m, style)

	if cfg.HasFavorites() || cfg.HasFavoritesV1() {
		m.toFavoritesTab()
	} else {
		m.toBrowseTab()
	}

	go m.statusHandler(ctx)
	return &m
}

func getVolumeBar(secondColor string) progress.Model {
	b := progress.New([]progress.Option{
		progress.WithWidth(10),
		progress.WithSolidFill(secondColor),
		progress.WithoutPercentage(),
	}...)
	b.EmptyColor = secondColor
	return b
}

func updatePlayerMetadata(ctx context.Context, progr *tea.Program, m *Model) {
	tick := time.NewTicker(playerPollInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			pollMetadata(m, progr)
		}
	}
}

func pollMetadata(m *Model, progr *tea.Program) {
	log := slog.With("method", "pollMetadata")

	m.delegate.playingMtx.RLock()
	currPlaying := m.delegate.currPlaying
	m.delegate.playingMtx.RUnlock()

	if currPlaying == nil {
		return
	}
	metadata := m.player.Metadata()
	if metadata == nil {
		return
	} else if metadata.Err != nil {
		log.Error("", "metadata", metadata.Err)
		return
	}
	msg := getMetadataMsg(*currPlaying, *metadata)
	go progr.Send(msg)
}

type Model struct {
	Progr *tea.Program

	ready    bool
	cfg      *config.Value
	style    *Style
	browser  *browser.API
	player   *player.Player
	delegate *stationDelegate

	tabs         []uiTab
	activeTabIdx uiTabIndex

	// display currently performed action or encountered error
	statusMsg    string
	statusUpdate chan struct{}

	// display station metadata
	playbackTime time.Duration
	spinner      *spinner.Model
	songTitle    string
	volumeBar    progress.Model

	width        int
	listWidth    int
	totHeight    int
	headerHeight int

	nowPlaying     *nowPlayingModel
	icyCancel      context.CancelFunc
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) startIcySniffer(ctx context.Context, s smodel.Station) tea.Cmd {
	return func() tea.Msg {
		// Security: Validate URL scheme
		if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
			return nil
		}

		ch, err := metadata.FetchIcyMetadata(ctx, s.URL)
		if err != nil {
			return nil
		}

		go func() {
			for meta := range ch {
				m.Progr.Send(metadataMsg{
					stationUUID: s.Stationuuid,
					stationName: s.Name,
					songTitle:   meta.StreamTitle,
				})
			}
		}()
		return nil
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	logTeaMsg(msg, "ui.model.Update")
	activeTab := m.tabs[m.activeTabIdx]

	if _, ok := msg.(eqTickMsg); ok {
		_, cmd := m.nowPlaying.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	//
	// messages that need to reach all tabs
	//
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.totHeight = msg.Height
		header := m.headerView(msg.Width)
		m.headerHeight = lipgloss.Height(header)
		var cmds []tea.Cmd

		h, v := m.style.DocStyle.GetFrameSize()
		usableWidth := msg.Width - h
		
		listWidth := usableWidth / 2
		nowPlayingWidth := usableWidth - listWidth

		m.listWidth = listWidth + h
		m.nowPlaying.width = nowPlayingWidth
		// Subtract headerHeight, DocStyle vertical padding (v), AND the 2-line separator
		m.nowPlaying.height = msg.Height - m.headerHeight - v - 2

		tabSizeMsg := tea.WindowSizeMsg{
			Width:  m.listWidth,
			Height: msg.Height - 2, // Account for separator lines
		}

		if !m.ready {
			m.ready = true
			for i := range m.tabs {
				tcmd := m.tabs[i].Init(m)
				cmds = append(cmds, tcmd)
			}
			cmds = append(cmds, m.nowPlaying.Init())
		} else {
			for i := range m.tabs {
				newTab, tcmd := m.tabs[i].Update(m, tabSizeMsg)
				m.tabs[i] = newTab
				cmds = append(cmds, tcmd)
			}
		}
		_, ncmd := m.nowPlaying.Update(msg)
		cmds = append(cmds, ncmd)
		return m, tea.Batch(cmds...)

	case quitMsg:
		return nil, tea.Quit

	case statusMsg:
		m.updateStatus(string(msg))
		// Recalculate header height if status changes
		header := m.headerView(m.width)
		newHeaderHeight := lipgloss.Height(header)
		if newHeaderHeight != m.headerHeight {
			m.headerHeight = newHeaderHeight
			return m, m.triggerResize()
		}
		return m, nil

	case clearStatusMsg:
		m.statusMsg = ""
		header := m.headerView(m.width)
		newHeaderHeight := lipgloss.Height(header)
		if newHeaderHeight != m.headerHeight {
			m.headerHeight = newHeaderHeight
			return m, m.triggerResize()
		}
		return m, nil

	case metadataMsg:
		// Deduplicate history entries
		if msg.songTitle != "" && msg.songTitle != m.songTitle {
			go m.cfg.AddHistoryEntry(
				time.Now(),
				strings.TrimSpace(msg.stationUUID),
				strings.TrimSpace(msg.stationName),
				strings.TrimSpace(msg.songTitle),
			)
		}
		m.songTitle = msg.songTitle
		if msg.playbackTime != nil {
			m.playbackTime = *msg.playbackTime
		}
		_, cmd := m.nowPlaying.Update(msg)
		return m, cmd

	case spinner.TickMsg:
		if m.spinner == nil {
			return m, nil
		}
		var cmd tea.Cmd
		s, cmd := m.spinner.Update(msg)
		m.spinner = &s
		return m, cmd

	//
	// messages that need to reach a particular tab
	//
	case topStationsRespMsg, searchRespMsg:
		newTab, cmd := m.tabs[browseTabIx].Update(m, msg)
		m.tabs[browseTabIx] = newTab
		return m, cmd

	case customStationRespMsg:
		newTab, cmd := m.tabs[favoriteTabIx].Update(m, msg)
		m.tabs[favoriteTabIx] = newTab
		return m, cmd

	case favoritesStationRespMsg:
		newTab, cmd := m.tabs[favoriteTabIx].Update(m, msg)
		m.tabs[favoriteTabIx] = newTab
		return m, cmd

	case toggleFavoriteMsg:
		newTab, cmd := m.tabs[favoriteTabIx].Update(m, msg)
		m.tabs[favoriteTabIx] = newTab
		return m, cmd

	case pauseRespMsg:
		if msg.err != "" {
			m.updateStatus(msg.err)
		} else {
			m.spinner = nil
			m.delegate.keymap.pause.SetHelp("space", "resume")
		}
		return m, nil

	case playRespMsg:
		if msg.err != "" {
			m.updateStatus(msg.err)
			m.spinner = nil
		}
		m.delegate.keymap.pause.SetHelp("space", "pause")
		_, cmd := m.nowPlaying.Update(msg)
		return m, cmd

	case logoFetchedMsg:
		_, cmd := m.nowPlaying.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		} else if activeTab, ok := activeTab.(filteringTab); ok && activeTab.IsFiltering() {
			break
		} else if activeTab, ok := activeTab.(stationTab); ok &&
			(activeTab.IsSearchEnabled() || activeTab.IsFiltering() || activeTab.IsCustomStationEnabled()) {
			break
		}

		d := m.delegate

		if key.Matches(msg, d.keymap.volumeDown) {
			return m, m.volumeCmd(false)
		}
		if key.Matches(msg, d.keymap.volumeUp) {
			return m, m.volumeCmd(true)
		}
		if key.Matches(msg, d.keymap.seekBack) {
			if m.activeTabIdx == settingsTabIx {
				newTab, cmd := m.tabs[settingsTabIx].Update(m, msg)
				m.tabs[settingsTabIx] = newTab
				return m, cmd
			}
			return m, m.seekCmd(-config.SeekStepSec)
		}
		if key.Matches(msg, d.keymap.seekFw) {
			if m.activeTabIdx == settingsTabIx {
				newTab, cmd := m.tabs[settingsTabIx].Update(m, msg)
				m.tabs[settingsTabIx] = newTab
				return m, cmd
			}
			return m, m.seekCmd(config.SeekStepSec)
		}

		if key.Matches(msg, d.keymap.pause) {
			if m.activeTabIdx == settingsTabIx {
				newTab, cmd := m.tabs[settingsTabIx].Update(m, msg)
				m.tabs[settingsTabIx] = newTab
				return m, cmd
			}

			if resM, resCmd := m.handlePauseKey(); resM != nil {
				return resM, resCmd
			}
			activeTab, ok := activeTab.(stationTab)
			if !ok {
				break
			}
			selStation, ok := activeTab.Stations().list.SelectedItem().(smodel.Station)
			if ok {
				return m, m.playStationCmd(selStation)
			}
		}

		if activeTab, ok := activeTab.(stationTab); ok && activeTab.IsInfoEnabled() {
			break
		}

		if key.Matches(msg, d.keymap.playSelected) {
			if m.activeTabIdx == settingsTabIx {
				newTab, cmd := m.tabs[settingsTabIx].Update(m, msg)
				m.tabs[settingsTabIx] = newTab
				return m, cmd
			}

			activeTab, ok := activeTab.(stationTab)
			if !ok {
				break
			}
			selStation, ok := activeTab.Stations().list.SelectedItem().(smodel.Station)
			if ok {
				return m, m.playStationCmd(selStation)
			}
		}
	}

	//
	// messages that need to reach active tab
	//
	activeTabIdx := m.activeTabIdx
	newTab, cmd := m.tabs[activeTabIdx].Update(m, msg)
	m.tabs[activeTabIdx] = newTab
	return m, cmd
}

func (m *Model) handlePauseKey() (*Model, tea.Cmd) {
	log := slog.With("method", "ui.Model.handlePauseKey")
	log.Info("begin")
	defer log.Info("end")

	m.delegate.playingMtx.RLock()
	defer m.delegate.playingMtx.RUnlock()

	if m.delegate.currPlaying != nil {
		return m, m.delegate.pauseCmd()
	} else if m.delegate.prevPlaying != nil {
		cmds := []tea.Cmd{m.initSpinner(), m.delegate.resumeCmd()}
		return m, tea.Batch(cmds...)
	}
	return nil, nil
}

func (m *Model) statusHandler(ctx context.Context) {
	t := time.NewTimer(math.MaxInt64)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Progr.Send(clearStatusMsg{})
		case <-m.statusUpdate:
			t.Stop()
			t.Reset(statusMsgTimeout)
		}
	}
}

func (m *Model) toFavoritesTab() {
	m.delegate.keymap.toggleFavorite.SetEnabled(false)
	m.delegate.keymap.toggleAutoplay.SetEnabled(true)
	m.activeTabIdx = favoriteTabIx
}

func (m *Model) toBrowseTab() {
	m.delegate.keymap.toggleFavorite.SetEnabled(true)
	m.delegate.keymap.toggleAutoplay.SetEnabled(false)
	m.activeTabIdx = browseTabIx
}

func (m *Model) toHistoryTab() {
	m.activeTabIdx = historyTabIx
}

func (m *Model) toSettingsTab() tea.Cmd {
	m.activeTabIdx = settingsTabIx
	st := m.tabs[settingsTabIx].(*settingsTab)
	return st.onEnter()
}

func (m *Model) updateStatus(msg string) {
	slog.Info("updateStatus", "old", m.statusMsg, "new", msg)
	m.statusMsg = msg
	go func() {
		m.statusUpdate <- struct{}{}
	}()
}

func (m *Model) Quit() {
	log := slog.With("method", "ui.model.quit")
	log.Info("----------------------Quitting----------------------")

	// stop player
	err := m.player.Stop()
	if err != nil {
		log.Error("player stop", "error", err.Error())
	}
	err = m.player.Close()
	if err != nil {
		slog.Error(fmt.Sprintf("player close error: %v", err))
	}

	// save config
	if !m.cfg.IsFavorite(m.cfg.AutoplayFavorite) {
		m.cfg.AutoplayFavorite = ""
	}
	st := m.tabs[settingsTabIx].(*settingsTab)
	st.updateConfig()

	err = m.cfg.Save()
	if err != nil {
		log.Info(fmt.Sprintf("config save err: %v", err))
	}
	log.Info("config saved")
}

func (m *Model) newSpinner() *spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Spinner{
		Frames: []string{"⡷", "⣧", "⣏", "⡟", "⡷", "⣧", "⣏", "⡟"},
		FPS:    time.Second / 10,
	}
	s.Style = m.style.SongTitleStyle
	return &s
}

func (m *Model) initSpinner() tea.Cmd {
	m.spinner = m.newSpinner()
	return m.spinner.Tick
}

func (m *Model) triggerResize() tea.Cmd {
	return func() tea.Msg {
		return tea.WindowSizeMsg{
			Width:  m.width,
			Height: m.totHeight,
		}
	}
}

func (m *Model) headerView(width int) string {
	var res strings.Builder
	status := ""
	if len(m.statusMsg) > 0 {
		// Truncate status to fit roughly 60% of the width to prevent wrapping
		maxStatusWidth := int(float64(width) * 0.6)
		statusStr := runewidth.Truncate(m.statusMsg, maxStatusWidth, "…")
		status = m.style.StatusBarStyle.Render(strings.Repeat(" ", HeaderPadDist) + statusStr)
	}
	res.WriteString(status)
	appNameVers := m.style.StatusBarStyle.Render(fmt.Sprintf("sonicradio v%v  ", m.cfg.Version))
	fill := max(0, width-lipgloss.Width(status)-lipgloss.Width(appNameVers)-2*HeaderPadDist)
	res.WriteString(m.style.StatusBarStyle.Render(strings.Repeat(" ", fill)))
	res.WriteString(appNameVers)
	res.WriteString("\n\n")

	metadata := m.metadataView(width)
	res.WriteString(metadata)

	res.WriteString("\n\n")

	var renderedTabs []string
	renderedTabs = append(renderedTabs, m.style.TabGap.Render(strings.Repeat(" ", TabGapDistance)))
	for i := range m.tabs {
		if i == int(m.activeTabIdx) {
			tabName := m.activeTabIdx.String()
			renderedTab := m.renderTabName(tabName, &m.style.ActiveTabInner, &m.style.ActiveTabInnerHighlight)
			renderedTabs = append(renderedTabs, m.style.ActiveTabBorder.Render(renderedTab.String()))
		} else {
			tabName := uiTabIndex(i).String()
			renderedTab := m.renderTabName(tabName, &m.style.InactiveTabInner, &m.style.InactiveTabInnerHighlight)
			renderedTabs = append(renderedTabs, m.style.InactiveTabBorder.Render(renderedTab.String()))
		}
		if i < len(m.tabs)-1 {
			renderedTabs = append(renderedTabs, m.style.TabGap.Render(strings.Repeat(" ", TabGapDistance)))
		}
	}
	row := lipgloss.JoinHorizontal(
		lipgloss.Top,
		renderedTabs...,
	)
	hFill := width - lipgloss.Width(row) - 2*HeaderPadDist
	gap := m.style.TabGap.Render(strings.Repeat(" ", max(0, hFill)))
	res.WriteString(lipgloss.JoinHorizontal(lipgloss.Bottom, row, gap))

	return res.String()
}

func (*Model) renderTabName(tabName string, tabInner *lipgloss.Style, tabInnerHighlight *lipgloss.Style) strings.Builder {
	highlight := false
	var renderTab strings.Builder
	for _, r := range tabName {
		rStr := fmt.Sprintf("%c", r)
		if unicode.IsSpace(r) {
			renderTab.WriteString(tabInner.Render(rStr))
		} else if !highlight {
			renderTab.WriteString(tabInnerHighlight.Render(rStr))
			highlight = true
		} else {
			renderTab.WriteString(tabInner.Render(rStr))
		}
	}
	return renderTab
}

func (m *Model) metadataView(width int) string {
	gap := strings.Repeat(" ", HeaderPadDist)

	volumeView := gap +
		m.volumeBar.ViewAs(float64(m.cfg.GetVolume())/100) +
		m.style.ItalicStyle.Render(fmt.Sprintf(volumeFmt, m.cfg.GetVolume(), gap))

	volumeW := lipgloss.Width(volumeView)
	maxW := max(0, width-volumeW)

	middleSpace := strings.Repeat(" ", maxW)

	return lipgloss.JoinHorizontal(lipgloss.Top, middleSpace, volumeView)
}

func (m Model) View() string {
	if !m.ready {
		return loadingMsg
	}

	var doc strings.Builder
	header := m.headerView(m.width)
	doc.WriteString(header)
	doc.WriteString("\n\n")

	tabView := m.tabs[m.activeTabIdx].View()
	npView := m.nowPlaying.View()

	body := lipgloss.JoinHorizontal(lipgloss.Top, tabView, npView)
	doc.WriteString(body)

	return m.style.DocStyle.MaxHeight(m.totHeight).Render(doc.String())
}

func (m *Model) changeStationView() {
	log := slog.With("method", "ui.Model.changeStationView")
	m.cfg.StationView = (m.cfg.StationView + 1) % 3
	log.Info(fmt.Sprintf("new stationView=%s", m.cfg.StationView.String()))
	m.delegate.setStationView(m.cfg.StationView)
	for tIx := range m.tabs {
		if st, ok := m.tabs[tIx].(stationTab); ok && st.Stations() != nil {
			st.Stations().list.SetDelegate(m.delegate)
		}
	}
}

func (m *Model) changeTheme(themeIdx int) {
	m.style.SetThemeIdx(themeIdx)
	m.cfg.Theme = themeIdx
	if m.spinner != nil {
		m.spinner.Style = m.style.SongTitleStyle
	}
	m.volumeBar.FullColor = m.style.GetSecondColor()
	m.volumeBar.EmptyColor = m.style.GetSecondColor()

	helpStyle := m.style.HelpStyles()
	for i := range m.tabs {
		if t, ok := m.tabs[i].(stationTab); ok {
			m.style.TextInputSyle(&t.Stations().list.FilterInput, stationsFilterPrompt, stationsFilterPlaceholder)
			t.Stations().list.Help.Styles = helpStyle
			t.Stations().list.Styles.HelpStyle = m.style.HelpStyle
			t.Stations().list.Styles.NoItems = m.style.NoItemsStyle
			t.Stations().infoModel.help.Styles = helpStyle

			if browse, ok := t.(*browseTab); ok {
				for iIdx := range browse.searchModel.textInputs {
					input := browse.searchModel.textInputs[iIdx].TextInput()
					m.style.TextInputSyle(input, input.Prompt, input.Placeholder)
					input.PromptStyle = m.style.PromptStyle
				}
				browse.searchModel.help.Styles = helpStyle
			} else if favorites, ok := t.(*favoritesTab); ok {
				for iIdx := range favorites.customStationModel.textInputs {
					input := favorites.customStationModel.textInputs[iIdx].TextInput()
					m.style.TextInputSyle(input, input.Prompt, input.Placeholder)
					input.PromptStyle = m.style.PromptStyle
				}
				favorites.customStationModel.help.Styles = helpStyle
			}

		} else if ht, ok := m.tabs[i].(*historyTab); ok {
			m.style.TextInputSyle(&ht.list.FilterInput, stationsFilterPrompt, historyFilterPlaceholder)
			ht.list.Help.Styles = helpStyle
			ht.list.Styles.HelpStyle = m.style.HelpStyle
			ht.list.Styles.NoItems = m.style.NoItemsStyle

		} else if st, ok := m.tabs[i].(*settingsTab); ok {
			for iIdx := range st.inputs {
				if st.inputs[iIdx] == nil || st.inputs[iIdx].TextInput() == nil {
					continue
				}
				input := st.inputs[iIdx].TextInput()
				m.style.TextInputSyle(input, input.Prompt, input.Placeholder)
				input.PromptStyle = m.style.PromptStyle
			}
			st.help.Styles = helpStyle
		}
	}
}

func trapSignal(p *tea.Program) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

	go func() {
		osCall := <-signals
		slog.Info(fmt.Sprintf("received OS signal %+v", osCall))
		p.Send(quitMsg{})
	}()
}

func logTeaMsg(msg tea.Msg, tag string) {
	log := slog.With("method", tag)
	switch msg.(type) {
	case favoritesStationRespMsg, topStationsRespMsg, searchRespMsg, customStationRespMsg, toggleInfoMsg:
		log.Info("tea.Msg", "type", fmt.Sprintf("%T", msg))
	case cursor.BlinkMsg, spinner.TickMsg, list.FilterMatchesMsg:
		break
	default:
		log.Info("tea.Msg", "type", fmt.Sprintf("%T", msg), "value", msg, "#", fmt.Sprintf("%#v", msg))
	}
}
