// Package ui implements loggy's full-screen dashboard.
package ui

import "github.com/charmbracelet/lipgloss"

var (
	colAccent = lipgloss.AdaptiveColor{Light: "#5A48D6", Dark: "#A496FF"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#7B7B86", Dark: "#7E7E8E"}
	colBorder = lipgloss.AdaptiveColor{Light: "#D3D3DC", Dark: "#3B3B4A"}
	colSelBg  = lipgloss.AdaptiveColor{Light: "#ECE9FF", Dark: "#2B2840"}
	colError  = lipgloss.AdaptiveColor{Light: "#C62828", Dark: "#FF6B6B"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#A15C00", Dark: "#F2C14E"}
	colInfo   = lipgloss.AdaptiveColor{Light: "#1E6FB8", Dark: "#7CB7FF"}
	colOK     = lipgloss.AdaptiveColor{Light: "#2E7D32", Dark: "#7BD88F"}
	colKey    = lipgloss.AdaptiveColor{Light: "#6A4FB3", Dark: "#C3B5FF"}
)

var (
	sMuted  = lipgloss.NewStyle().Foreground(colMuted)
	sBold   = lipgloss.NewStyle().Bold(true)
	sAccent = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	sError  = lipgloss.NewStyle().Foreground(colError)
	sWarn   = lipgloss.NewStyle().Foreground(colWarn)
	sOK     = lipgloss.NewStyle().Foreground(colOK)
	sJSONKy = lipgloss.NewStyle().Foreground(colKey)

	sBadgeErr = lipgloss.NewStyle().Foreground(colError).Bold(true)
	sBadgeWrn = lipgloss.NewStyle().Foreground(colWarn).Bold(true)
	sBadgeInf = lipgloss.NewStyle().Foreground(colInfo)
	sBadgeDbg = lipgloss.NewStyle().Foreground(colMuted)

	sPane        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colBorder).Padding(0, 1)
	sPaneFocused = sPane.BorderForeground(colAccent)

	sSelected = lipgloss.NewStyle().Background(colSelBg)
	sEnvPill  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#1A1A24"}).Background(colAccent).Bold(true).Padding(0, 1)
	sKey      = lipgloss.NewStyle().Foreground(colKey).Bold(true)
)
