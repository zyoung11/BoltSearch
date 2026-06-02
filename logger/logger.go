package logger

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// Catppuccin Macchiato palette
var (
	promptStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#8aadf4")).Render  // blue
	inputStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#8aadf4")).Render  // blue
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#8aadf4")).Render  // blue
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#a6da95")).Render  // green
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#eed49f")).Render  // yellow
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ed8796")).Render  // red
)

type Logger struct{}

func (Logger) Prompt(msg string)  { fmt.Println(promptStyle("[?] " + msg)) }
func (Logger) Input(msg string)   { fmt.Print(inputStyle("[>] " + msg)) }
func (Logger) Info(msg string)    { fmt.Println(infoStyle("[i] " + msg)) }
func (Logger) Success(msg string) { fmt.Println(successStyle("[+] " + msg)) }
func (Logger) Warn(msg string)    { fmt.Println(warnStyle("[!] " + msg)) }
func (Logger) Error(msg string)   { fmt.Println(errorStyle("[-] " + msg)) }

// Styled prefixes for direct use with fmt.Printf in main.go
var (
	InfoPrefix    = infoStyle("[i]")
	WarnPrefix    = warnStyle("[!]")
	ErrorPrefix   = errorStyle("[-]")
	SuccessPrefix = successStyle("[+]")
)
