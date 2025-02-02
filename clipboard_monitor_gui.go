package main

import (
    "encoding/json"
    "fmt"
    "log"
    "os"
    "regexp"
    "time"

    "github.com/atotto/clipboard"
    "fyne.io/fyne/v2"
    "fyne.io/fyne/v2/app"
    "fyne.io/fyne/v2/container"
    "fyne.io/fyne/v2/widget"
)

type Rule struct {
    Pattern     string         `json:"pattern"`
    Replacement string         `json:"replacement"`
    Enabled     bool           `json:"enabled"`
    Regexp      *regexp.Regexp `json:"-"` // Скомпилированное регулярное выражение
}

var (
    monitoring       = false
    replacementRules []Rule
    monitorInterval  time.Duration = 500 * time.Millisecond
    rulesList        *widget.List
    lastText         string
)

const maxTextSize = 512 * 512 // 0.5 MB

func isTextContent(content string) bool {
    return len(content) > 0 && !regexp.MustCompile(`[\x00-\x08\x0B\x0C\x0E-\x1F]`).MatchString(content)
}

func isTextTooLarge(content string) bool {
    return len(content) > maxTextSize
}

func loadSettings() {
    file, err := os.ReadFile("settings.json")
    if err != nil {
        log.Println("Settings file not found, using default values.")
        replacementRules = []Rule{}
        return
    }

    var settings struct {
        Rules    []Rule `json:"rules"`
        Interval int    `json:"interval_ms"`
    }
    if err := json.Unmarshal(file, &settings); err != nil {
        log.Println("Error reading settings file:", err)
        return
    }

    replacementRules = settings.Rules
    for i := range replacementRules {
        if replacementRules[i].Pattern != "" {
            re, err := regexp.Compile(replacementRules[i].Pattern)
            if err != nil {
                log.Printf("Invalid regex in rule %d: %v\n", i, err)
                continue
            }
            replacementRules[i].Regexp = re
        }
    }

    monitorInterval = time.Duration(settings.Interval) * time.Millisecond
}

func saveSettings() {
    settings := struct {
        Rules    []Rule `json:"rules"`
        Interval int    `json:"interval_ms"`
    }{
        Rules:    replacementRules,
        Interval: int(monitorInterval.Milliseconds()),
    }
    data, err := json.MarshalIndent(settings, "", "  ")
    if err != nil {
        log.Println("Error saving settings:", err)
        return
    }
    os.WriteFile("settings.json", data, 0644)
}

func writeToClipboard(data string) error {
    maxAttempts := 2
    for i := 0; i < maxAttempts; i++ {
        err := clipboard.WriteAll(data)
        if err == nil {
            return nil
        }
        log.Printf("Attempt %d failed to write to clipboard: %v\n", i+1, err)
        time.Sleep(200 * time.Millisecond)
    }
    return fmt.Errorf("failed to write to clipboard after %d attempts", maxAttempts)
}

func applyRules(currentText string) string {
    for _, rule := range replacementRules {
        if rule.Enabled && rule.Regexp != nil {
            currentText = rule.Regexp.ReplaceAllString(currentText, rule.Replacement)
        }
    }
    return currentText
}

func monitorClipboard(statusLabel *widget.Label) {
    for {
        if !monitoring {
            time.Sleep(200 * time.Millisecond)
            continue
        }

        currentText, err := clipboard.ReadAll()
        if err != nil {
            // Проверяем, является ли ошибка связанной с не текстовыми данными
            if err.Error() == "The operation completed successfully." {
                statusLabel.SetText("Non-text content detected, skipping")
                time.Sleep(monitorInterval)
                continue
            }
            log.Println("Error reading clipboard:", err)
            continue
        }

        // Проверяем, является ли содержимое текстом
        if !isTextContent(currentText) {
            statusLabel.SetText("Non-text content detected, skipping")
            time.Sleep(monitorInterval)
            continue
        }

        // Проверяем размер текста
        if isTextTooLarge(currentText) {
            statusLabel.SetText("Text too large, skipping")
            time.Sleep(monitorInterval)
            continue
        }

        if currentText != lastText {
            updatedText := applyRules(currentText)
            if updatedText != currentText {
                err := writeToClipboard(updatedText)
                if err != nil {
                    statusLabel.SetText("Failed to write to clipboard")
                    log.Println("Error writing to clipboard:", err)
                } else {
                    statusLabel.SetText("Successfully wrote to clipboard")
                    lastText = updatedText
                }
            }
        }
        time.Sleep(monitorInterval)
    }
}

func createUI(myApp fyne.App) fyne.Window {
    myWindow := myApp.NewWindow("Clipboard Monitor")
    statusLabel := widget.NewLabel("Status: Waiting for actions")
    rulePatternEntry := widget.NewEntry()
    rulePatternEntry.SetPlaceHolder("Enter pattern")
    ruleReplacementEntry := widget.NewEntry()
    ruleReplacementEntry.SetPlaceHolder("Enter replacement")

    // Инициализируем список правил
    rulesList = widget.NewList(
        func() int { return len(replacementRules) },
        func() fyne.CanvasObject {
            return container.NewHBox(
                widget.NewCheck("", nil),
                widget.NewButton("Delete", nil),
            )
        },
        func(id widget.ListItemID, item fyne.CanvasObject) {
            box := item.(*fyne.Container)
            check := box.Objects[0].(*widget.Check)
            deleteButton := box.Objects[1].(*widget.Button)
            check.SetText(fmt.Sprintf("%s -> %s", replacementRules[id].Pattern, replacementRules[id].Replacement))
            check.SetChecked(replacementRules[id].Enabled)
            check.OnChanged = func(checked bool) {
                replacementRules[id].Enabled = checked
                saveSettings()
            }
            deleteButton.OnTapped = func() {
                replacementRules = append(replacementRules[:id], replacementRules[id+1:]...)
                saveSettings()
                rulesList.Refresh()
            }
        },
    )

    addRuleButton := widget.NewButton("Add", func() {
        if rulePatternEntry.Text == "" || ruleReplacementEntry.Text == "" {
            return
        }
        newRule := Rule{
            Pattern:     rulePatternEntry.Text,
            Replacement: ruleReplacementEntry.Text,
            Enabled:     true,
        }
        re, err := regexp.Compile(newRule.Pattern)
        if err != nil {
            log.Printf("Invalid regex: %v\n", err)
            return
        }
        newRule.Regexp = re
        replacementRules = append(replacementRules, newRule)
        saveSettings()
        rulesList.Refresh()
        rulePatternEntry.SetText("")
        ruleReplacementEntry.SetText("")
    })

    startButton := widget.NewButton("▶ Start", func() {
        monitoring = true
        statusLabel.SetText("Monitoring started")
    })
    stopButton := widget.NewButton("■ Stop", func() {
        monitoring = false
        statusLabel.SetText("Monitoring stopped")
    })

    // Создаем контейнер с ограниченной высотой для списка правил
    rulesContainer := container.NewVScroll(rulesList)
    rulesContainer.SetMinSize(fyne.NewSize(600, 150)) // Ограничиваем высоту списка

    // Инициализация интерфейса
    myWindow.SetContent(container.NewVBox(
        widget.NewLabel("📋 Clipboard Monitoring"),
        container.NewHBox(startButton, stopButton),
        statusLabel,
        widget.NewLabel("➕ Add Rule:"),
        rulePatternEntry,
        ruleReplacementEntry,
        addRuleButton,
        widget.NewLabel("📌 Rules List:"),
        rulesContainer, // Используем контейнер с прокруткой
    ))
    myWindow.Resize(fyne.NewSize(650, 450))
    return myWindow
}

func main() {
    loadSettings()
    myApp := app.New()
    myWindow := createUI(myApp)
    statusLabel := widget.NewLabel("Status: Waiting for actions")
    go monitorClipboard(statusLabel)
    myWindow.ShowAndRun()
}