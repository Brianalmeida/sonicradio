package ui

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/dancnb/sonicradio/browser"
	"github.com/dancnb/sonicradio/config"
	"github.com/dancnb/sonicradio/model"
	"github.com/dancnb/sonicradio/player"
)

func newStationDelegate(cfg *config.Value, s *Style, p *player.Player, b *browser.API) *stationDelegate {
	keymap := newDelegateKeyMap()

	d := list.NewDefaultDelegate()

	st := &stationDelegate{
		player:          p,
		b:               b,
		cfg:             cfg,
		style:           s,
		keymap:          keymap,
		DefaultDelegate: d,
	}
	st.setStationView(cfg.StationView)
	return st
}

type stationDelegate struct {
	list.DefaultDelegate
	player *player.Player
	b      *browser.API
	cfg    *config.Value
	style  *Style

	playingMtx  sync.RWMutex
	actionMtx   sync.Mutex
	prevPlaying *model.Station
	currPlaying *model.Station

	deleted *model.Station

	keymap *delegateKeyMap
}

func (d *stationDelegate) setStationView(v config.StationView) {
	switch v {
	case config.DefaultView:
		d.DefaultDelegate.SetHeight(2)
		d.DefaultDelegate.SetSpacing(1)
	case config.CompactView:
		d.DefaultDelegate.SetHeight(1)
		d.DefaultDelegate.SetSpacing(1)
	case config.MinimalView:
		d.DefaultDelegate.SetHeight(1)
		d.DefaultDelegate.SetSpacing(0)
	}
}

func (d *stationDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	s, ok := item.(model.Station)
	if !ok {
		return
	}

	d.playingMtx.RLock()
	isPlaying := d.currPlaying != nil && d.currPlaying.Stationuuid == s.Stationuuid
	d.playingMtx.RUnlock()

	isSel := index == m.Index()

	prefix := IndexString(index + 1)

	itStyle := d.style.SecondaryColorStyle
	descStyle := d.style.PrimaryColorStyle
	prefixStyle := d.style.PrefixStyle

	if isSel {
		itStyle = d.style.SelItemStyle
		descStyle = d.style.SelDescStyle
		prefixStyle = d.style.SelectedBorderStyle
		if isPlaying {
			itStyle = d.style.SelNowPlayingStyle
			descStyle = d.style.SelNowPlayingDescStyle
		}
	} else if isPlaying {
		itStyle = d.style.SongTitleStyle
		prefixStyle = d.style.NowPlayingPrefixStyle
	}

	name := s.Name
	if d.cfg.IsFavorite(s.Stationuuid) {
		name += FavChar
	}
	if d.cfg.AutoplayFavorite == s.Stationuuid {
		name += AutoplayChar
	}

	var res string
	switch d.cfg.StationView {
	case config.DefaultView:
		res = d.renderDefaultView(prefix, name, s.Homepage, m.Width(), 0, prefixStyle, itStyle, descStyle)
	case config.CompactView:
		res = d.renderCompactView(prefix, name, s.Homepage, m.Width(), 0, prefixStyle, itStyle, descStyle)
	case config.MinimalView:
		res = d.renderMinimalView(prefix, name, s.Homepage, m.Width(), 0, prefixStyle, itStyle, descStyle)
	}

	_, _ = fmt.Fprint(w, res)
}

func (d *stationDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	logTeaMsg(msg, "ui.stationDelegate.Update")
	selStation, ok := m.SelectedItem().(model.Station)
	if !ok {
		return nil
	}

	if msg, ok := msg.(tea.KeyMsg); ok {
		isSel := m.Index() != -1
		switch {
		case key.Matches(msg, d.keymap.toggleFavorite):
			if !isSel {
				break
			}
			added := d.cfg.ToggleFavorite(selStation)
			return func() tea.Msg { return toggleFavoriteMsg{added, selStation} }
		case key.Matches(msg, d.keymap.toggleAutoplay):
			if !isSel {
				break
			}
			if d.cfg.AutoplayFavorite == selStation.Stationuuid {
				d.cfg.AutoplayFavorite = ""
			} else {
				d.cfg.AutoplayFavorite = selStation.Stationuuid
			}

		case key.Matches(msg, d.keymap.delete):
			if !isSel {
				break
			}
			idx := m.Index()
			m.RemoveItem(idx)
			d.deleted = &selStation

		case key.Matches(msg, d.keymap.pasteAfter):
			if !d.shouldPaste(m) {
				break
			}
			idx := m.Index()
			if len(m.Items()) > 0 {
				idx++
				m.Select(idx)
			}
			cmd := m.InsertItem(idx, *d.deleted)
			d.deleted = nil
			return cmd

		case key.Matches(msg, d.keymap.pasteBefore):
			if !d.shouldPaste(m) {
				break
			}
			idx := m.Index()
			cmd := m.InsertItem(idx, *d.deleted)
			d.deleted = nil
			return cmd
		}
	}

	return nil
}

func (d *stationDelegate) shouldPaste(m *list.Model) bool {
	if d.deleted == nil {
		return false
	}
	for _, it := range m.Items() {
		if s, ok := it.(model.Station); ok && s.Stationuuid == d.deleted.Stationuuid {
			return false
		}
	}
	return true
}

func (d *stationDelegate) pauseCmd() tea.Cmd {
	return func() tea.Msg {
		log := slog.With("method", "ui.stationDelegate.pauseCmd")
		log.Info("begin")
		defer log.Info("end")

		d.actionMtx.Lock()
		defer d.actionMtx.Unlock()

		d.playingMtx.RLock()
		curr := d.currPlaying
		d.playingMtx.RUnlock()

		if curr == nil {
			return nil
		}
		err := d.player.Pause(true)
		if err != nil {
			log.Error(fmt.Sprintf("player pause: %v", err))
			return pauseRespMsg{fmt.Sprintf("Could not pause station %s (%s)!", curr.Name, curr.URL)}
		}

		d.playingMtx.Lock()
		d.prevPlaying = d.currPlaying
		d.currPlaying = nil
		d.playingMtx.Unlock()

		return pauseRespMsg{}
	}
}

func (d *stationDelegate) resumeCmd() tea.Cmd {
	return func() tea.Msg {
		log := slog.With("method", "ui.stationDelegate.resumeCmd")
		log.Info("begin")
		defer log.Info("end")

		d.actionMtx.Lock()
		defer d.actionMtx.Unlock()

		d.playingMtx.RLock()
		prev := d.prevPlaying
		d.playingMtx.RUnlock()

		if prev == nil {
			return nil
		}
		err := d.player.Pause(false)
		if err != nil {
			log.Error(fmt.Sprintf("player resume: %v", err))
			return pauseRespMsg{fmt.Sprintf("Could not resume station %s (%s)!", prev.Name, prev.URL)}
		}

		d.playingMtx.Lock()
		d.currPlaying = d.prevPlaying
		d.playingMtx.Unlock()

		return pauseRespMsg{}
	}
}

func (d *stationDelegate) playCmd(s model.Station) tea.Cmd {
	return func() tea.Msg {
		log := slog.With("method", "ui.stationDelegate.playCmd")
		log.Info("begin")
		defer log.Info("end")

		// Serialize player backend actions to prevent data races and zombies
		d.actionMtx.Lock()
		defer d.actionMtx.Unlock()

		log.Info("playing", "id", s.Stationuuid)
		if !s.IsCustom {
			go d.increaseCounter(s)
		}

		err := d.player.Play(s.URL)
		if err != nil {
			errMsg := fmt.Sprintf("error playing station %s: %s", s.Name, err.Error())
			log.Error(errMsg)
			return playRespMsg{fmt.Sprintf("Could not start playback for %s: %s", s.Name, err.Error())}
		}

		d.playingMtx.Lock()
		d.prevPlaying = d.currPlaying
		d.currPlaying = &s
		d.playingMtx.Unlock()

		return playRespMsg{}
	}
}

func (d *stationDelegate) increaseCounter(station model.Station) {
	log := slog.With("method", "ui.stationDelegate.increaseCounter")
	err := d.b.StationCounter(station.Stationuuid)
	if err != nil {
		log.Error(err.Error())
	}
}

func (d *stationDelegate) renderDefaultView(
	prefix string,
	name string,
	desc string,
	listWidth int,
	widthOffset int,
	prefixStyle lipgloss.Style,
	itStyle lipgloss.Style,
	descStyle lipgloss.Style,
) string {
	var res strings.Builder
	prefixRender := prefixStyle.Render(prefix)
	res.WriteString(prefixRender)
	maxWidth := max(listWidth-lipgloss.Width(prefixRender)-HeaderPadDist-widthOffset, 0)

	name = runewidth.Truncate(name, maxWidth-widthOffset, "…")
	nameRender := itStyle.Render(name)
	res.WriteString(nameRender)
	hFill := max(listWidth-lipgloss.Width(prefixRender)-lipgloss.Width(nameRender)-HeaderPadDist-widthOffset, 0)
	res.WriteString(itStyle.Render(strings.Repeat(" ", hFill)))
	res.WriteString("\n")

	res.WriteString(prefixStyle.Render(strings.Repeat(" ", utf8.RuneCountInString(prefix))))
	desc = runewidth.Truncate(desc, maxWidth-widthOffset, "…")
	descRender := descStyle.Render(desc)
	res.WriteString(descRender)
	hFill = max(listWidth-lipgloss.Width(prefixRender)-lipgloss.Width(descRender)-HeaderPadDist-widthOffset, 0)
	res.WriteString(descStyle.Render(strings.Repeat(" ", hFill)))

	return res.String()
}

func (d *stationDelegate) renderCompactView(
	prefix string,
	name string,
	desc string,
	listWidth int,
	widthOffset int,
	prefixStyle lipgloss.Style,
	itStyle lipgloss.Style,
	descStyle lipgloss.Style,
) string {
	var res strings.Builder
	prefixRender := prefixStyle.Render(prefix)
	res.WriteString(prefixRender)
	maxWidth := max(listWidth-lipgloss.Width(prefixRender)-HeaderPadDist-widthOffset, 0)
	width1 := 45
	width2 := maxWidth - width1

	name = runewidth.Truncate(name, width1, "…")
	nameRender := itStyle.Render(name)
	res.WriteString(nameRender)
	hFill := max(width1-lipgloss.Width(nameRender), 0)
	res.WriteString(itStyle.Render(strings.Repeat(" ", hFill)))

	desc = runewidth.Truncate(desc, width2, "…")
	descRender := descStyle.Render(desc)
	res.WriteString(descRender)
	hFill = max(width2-lipgloss.Width(descRender), 0)
	res.WriteString(descStyle.Render(strings.Repeat(" ", hFill)))

	return res.String()
}

func (d *stationDelegate) renderMinimalView(
	prefix string,
	name string,
	desc string,
	listWidth int,
	widthOffset int,
	prefixStyle lipgloss.Style,
	itStyle lipgloss.Style,
	descStyle lipgloss.Style,
) string {
	var res strings.Builder
	prefixRender := prefixStyle.Render(prefix)
	res.WriteString(prefixRender)
	maxWidth := max(listWidth-lipgloss.Width(prefixRender)-HeaderPadDist, 0)

	name = runewidth.Truncate(name, maxWidth-widthOffset, "…")
	nameRender := itStyle.Render(name)
	res.WriteString(nameRender)
	hFill := max(listWidth-lipgloss.Width(prefixRender)-lipgloss.Width(nameRender)-HeaderPadDist-widthOffset, 0)
	res.WriteString(itStyle.Render(strings.Repeat(" ", hFill)))

	return res.String()
}

type delegateKeyMap struct {
	toggleFavorite key.Binding
	toggleAutoplay key.Binding
	delete         key.Binding
	pasteAfter     key.Binding
	pasteBefore    key.Binding
	volumeDown     key.Binding
	volumeUp       key.Binding
	seekBack       key.Binding
	seekFw         key.Binding
	pause          key.Binding
	playSelected   key.Binding
}

func newDelegateKeyMap() *delegateKeyMap {
	return &delegateKeyMap{
		toggleFavorite: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "toggle favorite"),
		),
		toggleAutoplay: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "toggle autoplay"),
		),
		delete: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "delete"),
		),
		pasteAfter: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "paste after"),
		),
		pasteBefore: key.NewBinding(
			key.WithKeys("P"),
			key.WithHelp("P", "paste before"),
		),
		volumeDown: key.NewBinding(
			key.WithKeys("-"),
			key.WithHelp("-", "volume -"),
		),
		volumeUp: key.NewBinding(
			key.WithKeys("+", "="),
			key.WithHelp("+", "volume +"),
		),
		seekBack: key.NewBinding(
			key.WithKeys("left", "h"),
			key.WithHelp("h/left", "seek -"),
		),
		seekFw: key.NewBinding(
			key.WithKeys("right", "l"),
			key.WithHelp("l/right", "seek +"),
		),
		pause: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "pause"),
		),
		playSelected: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "play"),
		),
	}
}
