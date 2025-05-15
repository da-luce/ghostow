package stringutil

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-runewidth"
)

// AskForConfirmation prompts the user with the given message and expects y/n input.
// Returns true if user types 'y' (case-insensitive).
func AskForConfirmation(prompt string) bool {
	bold := color.New(color.Bold).SprintFunc()
	fmt.Printf("%s [y/%s]: ", prompt, bold("N"))

	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))

	return answer == "y"
}

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// StripANSI removes ANSI escape codes from the input string.
func StripANSI(str string) string {
	return ansiRegex.ReplaceAllString(str, "")
}

// PrintDotTable prints rows of left/right strings with dots filling the gap.
// Each row is [2]string: left column and right column.
func PrintDotTable(rows [][2]string) {
	maxLeftLen := 0
	for _, row := range rows {
		if runewidth.StringWidth(row[0]) > maxLeftLen {
			maxLeftLen = runewidth.StringWidth(row[0])
		}
	}

	maxRightLen := 0
	for _, row := range rows {
		if runewidth.StringWidth(StripANSI(row[1])) > maxRightLen {
			maxRightLen = runewidth.StringWidth(StripANSI(row[1]))
		}
	}

	spacingLeft := 1
	spacingRight := 1
	extraDots := 5

	totalPadding := spacingLeft + spacingRight + extraDots

	divider := strings.Repeat("⎯", maxLeftLen+totalPadding+maxRightLen)
	fmt.Println(divider)

	leftSpace := strings.Repeat(" ", spacingLeft)
	rightSpace := strings.Repeat(" ", spacingRight)

	for _, row := range rows {
		left, right := row[0], row[1]
		numDots := maxLeftLen - runewidth.StringWidth(left) + extraDots
		dots := strings.Repeat(".", numDots)
		fmt.Printf("%s%s%s%s%s\n", left, leftSpace, dots, rightSpace, right)
	}
	fmt.Println(divider)
}
