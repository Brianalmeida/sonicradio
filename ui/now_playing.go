package ui

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/dancnb/sonicradio/browser"
)

type eqTickMsg time.Time

func tickEQ() tea.Cmd {
	return tea.Tick(time.Millisecond*200, func(t time.Time) tea.Msg {
		return eqTickMsg(t)
	})
}

type nowPlayingModel struct {
	width   int
	height  int
	style   *Style
	m       *Model
	eqChars []string
	eqState []int

	logo             string
	stationUUID      string
	lastFetchedTitle string
	artworkCancel    context.CancelFunc
}

func newNowPlayingModel(m *Model, style *Style) *nowPlayingModel {
	return &nowPlayingModel{
		style:   style,
		m:       m,
		eqChars: []string{" ", "▂", "▃", "▄", "▅", "▆", "▇", "█"},
		eqState: make([]int, 20),
		logo:    DefaultASCIIIcon(),
	}
}

func (n *nowPlayingModel) Init() tea.Cmd {
	// Initialize random state
	for i := range n.eqState {
		n.eqState[i] = rand.Intn(len(n.eqChars))
	}
	return tickEQ()
}

func (n *nowPlayingModel) Update(msg tea.Msg) (*nowPlayingModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case metadataMsg:
		if msg.songTitle != "" && msg.songTitle != n.lastFetchedTitle {
			n.lastFetchedTitle = msg.songTitle
			// Cancel in-flight artwork requests
			if n.artworkCancel != nil {
				n.artworkCancel()
			}
			ctx, cancel := context.WithCancel(context.Background())
			n.artworkCancel = cancel
			cmds = append(cmds, n.fetchArtworkCmd(ctx, msg.songTitle))
		}

	case logoFetchedMsg:
		n.m.delegate.playingMtx.RLock()
		isMatch := (n.m.delegate.currPlaying != nil && (n.m.delegate.currPlaying.Favicon == msg.url || n.lastFetchedTitle == msg.url)) ||
			(n.m.delegate.currPlaying == nil && n.m.delegate.prevPlaying != nil && n.m.delegate.prevPlaying.Favicon == msg.url)
		n.m.delegate.playingMtx.RUnlock()
		if isMatch {
			n.logo = msg.logo
		}

	case playRespMsg:
		n.m.delegate.playingMtx.RLock()
		if n.m.delegate.currPlaying != nil {
			url := n.m.delegate.currPlaying.Favicon
			name := n.m.delegate.currPlaying.Name
			n.stationUUID = n.m.delegate.currPlaying.Stationuuid
			n.lastFetchedTitle = "" // Reset on new station
			// Cancel station logo if we switch quickly
			if n.artworkCancel != nil {
				n.artworkCancel()
			}
			ctx, cancel := context.WithCancel(context.Background())
			n.artworkCancel = cancel

			n.m.delegate.playingMtx.RUnlock()
			if url != "" {
				cmds = append(cmds, n.fetchLogoCmd(ctx, url))
			} else {
				n.lastFetchedTitle = name
				cmds = append(cmds, n.fetchArtworkCmd(ctx, name))
			}
		} else {
			n.m.delegate.playingMtx.RUnlock()
		}

	case eqTickMsg:
		n.m.delegate.playingMtx.RLock()
		isPlaying := n.m.delegate.currPlaying != nil
		if n.m.delegate.currPlaying != nil && n.m.delegate.currPlaying.Stationuuid != n.stationUUID {
			// Station changed but we might have missed playRespMsg or it's a resume
			url := n.m.delegate.currPlaying.Favicon
			n.stationUUID = n.m.delegate.currPlaying.Stationuuid
			
			if n.artworkCancel != nil {
				n.artworkCancel()
			}
			ctx, cancel := context.WithCancel(context.Background())
			n.artworkCancel = cancel
			cmds = append(cmds, n.fetchLogoCmd(ctx, url))
		}
		n.m.delegate.playingMtx.RUnlock()

		if isPlaying {
			for i := range n.eqState {
				n.eqState[i] = rand.Intn(len(n.eqChars))
			}
		} else {
			for i := range n.eqState {
				n.eqState[i] = 0
			}
		}
		cmds = append(cmds, tickEQ())
	}
	return n, tea.Batch(cmds...)
}

func (n *nowPlayingModel) fetchArtworkCmd(ctx context.Context, term string) tea.Cmd {
	return func() tea.Msg {
		artworkURL, err := browser.FetchArtworkURL(ctx, term)
		if err != nil || artworkURL == "" {
			return nil
		}
		logo := FetchAndConvertLogo(ctx, artworkURL)
		return logoFetchedMsg{url: term, logo: logo}
	}
}

func (n *nowPlayingModel) fetchLogoCmd(ctx context.Context, url string) tea.Cmd {
	return func() tea.Msg {
		logo := FetchAndConvertLogo(ctx, url)
		return logoFetchedMsg{url: url, logo: logo}
	}
}

func (n *nowPlayingModel) View() string {
	n.m.delegate.playingMtx.RLock()
	defer n.m.delegate.playingMtx.RUnlock()

	var songName string
	var subtext string

	if n.m.delegate.currPlaying != nil {
		songName = n.m.delegate.currPlaying.Name
		subtext = n.m.songTitle
		if subtext == "" {
			subtext = n.m.delegate.currPlaying.Homepage
		}
	} else if n.m.delegate.prevPlaying != nil {
		songName = n.m.delegate.prevPlaying.Name
		subtext = n.m.delegate.prevPlaying.Homepage
	} else {
		songName = noPlayingMsg
		n.logo = DefaultASCIIIcon()
	}

	logoBlock := n.logo
	if n.logo == DefaultASCIIIcon() {
		logoBlock = n.style.SecondaryColorStyle.Render(n.logo)
	}

	var eq strings.Builder
	for _, val := range n.eqState {
		eq.WriteString(n.eqChars[val])
	}
	eqBlock := n.style.PrimaryColorStyle.Render(eq.String())

	// Truncate to panel width minus some padding
	maxW := max(0, n.width-4)
	songName = runewidth.Truncate(songName, maxW, "…")
	subtext = runewidth.Truncate(subtext, maxW, "…")

	songBlock := n.style.SongTitleStyle.Render(songName)
	subtextBlock := n.style.PrimaryColorStyle.Render(subtext)

	// Play time string
	timeStr := ""
	if n.m.playbackTime > 0 {
		h := int(n.m.playbackTime.Hours())
		m := int(n.m.playbackTime.Minutes()) % 60
		s := int(n.m.playbackTime.Seconds()) % 60
		timeStr = n.style.ItalicStyle.Render(fmt.Sprintf("%03d:%02d:%02d", h, m, s))
	}

	content := lipgloss.JoinVertical(lipgloss.Center, logoBlock, "", eqBlock, "", songBlock, subtextBlock, timeStr)

	// Ensure content height does not exceed n.height to prevent terminal scrolling
	contentHeight := lipgloss.Height(content)
	if contentHeight > n.height {
		// If too tall, we might need to remove some spacers or the time string
		// For now, let's just use the centered placement and trust lipgloss.Place
	}

	return lipgloss.Place(n.width, n.height, lipgloss.Center, lipgloss.Center, content)
}
