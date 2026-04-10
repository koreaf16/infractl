// Package tui
// File: server_table.go
// Description: /servers 슬래시 명령용 박스-드로잉 테이블 렌더링
// Responsibility: 서버 목록을 박스-드로잉 문자 + 활성 서버 표시로 포맷팅

package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/yourorg/infractl/internal/store"
)

// buildServerTable은 서버 목록을 박스-드로잉 테이블 문자열로 반환한다.
// active가 nil이 아니면 해당 서버 행에 ● 마커를 표시한다.
func buildServerTable(servers []store.Server, active *store.Server) string {
	type tableRow struct {
		name, host, port, user, os string
		isActive                   bool
	}

	// localhost 가상 행 + 등록 서버 행
	rows := []tableRow{{name: "localhost", host: "(this machine)", port: "-", user: "-"}}
	for _, s := range servers {
		rows = append(rows, tableRow{
			name:     s.Name,
			host:     s.Host,
			port:     strconv.Itoa(s.Port),
			user:     s.User,
			os:       s.OS,
			isActive: active != nil && s.Name == active.Name,
		})
	}

	// 열 너비 계산 (헤더 기준 최솟값)
	wName := len("NAME")
	wHost := len("HOST")
	wPort := len("PORT")
	wUser := len("USER")
	wOS := len("OS")
	showOS := false

	for _, r := range rows {
		if len(r.name) > wName {
			wName = len(r.name)
		}
		if len(r.host) > wHost {
			wHost = len(r.host)
		}
		if len(r.port) > wPort {
			wPort = len(r.port)
		}
		if len(r.user) > wUser {
			wUser = len(r.user)
		}
		if r.os != "" {
			showOS = true
			if len(r.os) > wOS {
				wOS = len(r.os)
			}
		}
	}
	// 이름 열 앞에 "● " 또는 "  " 2자 예약
	wName += 2

	pad := func(s string, w int) string {
		if len(s) >= w {
			return s[:w]
		}
		return s + strings.Repeat(" ", w-len(s))
	}

	hLine := func(left, mid, right string) string {
		var sb strings.Builder
		sb.WriteString(left)
		sb.WriteString(strings.Repeat("─", wName+2))
		sb.WriteString(mid)
		sb.WriteString(strings.Repeat("─", wHost+2))
		sb.WriteString(mid)
		sb.WriteString(strings.Repeat("─", wPort+2))
		sb.WriteString(mid)
		sb.WriteString(strings.Repeat("─", wUser+2))
		if showOS {
			sb.WriteString(mid)
			sb.WriteString(strings.Repeat("─", wOS+2))
		}
		sb.WriteString(right)
		return sb.String()
	}

	cell := func(s string, w int) string { return " " + pad(s, w) + " │" }

	nameCell := func(r tableRow, isHeader bool) string {
		if isHeader {
			return " " + pad("NAME", wName) + " │"
		}
		if r.isActive {
			dot := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("●")
			// " ● name..." : leading space + dot(1 vis) + space + name padded = wName visible total
			return " " + dot + " " + pad(r.name, wName-2) + " │"
		}
		return " " + pad(r.name, wName) + " │"
	}

	rowLine := func(r tableRow, isHeader bool) string {
		var sb strings.Builder
		sb.WriteString("│")
		sb.WriteString(nameCell(r, isHeader))
		if isHeader {
			sb.WriteString(cell("HOST", wHost))
			sb.WriteString(cell("PORT", wPort))
			sb.WriteString(cell("USER", wUser))
			if showOS {
				sb.WriteString(cell("OS", wOS))
			}
		} else {
			sb.WriteString(cell(r.host, wHost))
			sb.WriteString(cell(r.port, wPort))
			sb.WriteString(cell(r.user, wUser))
			if showOS {
				sb.WriteString(cell(r.os, wOS))
			}
		}
		return sb.String()
	}

	var sb strings.Builder
	sb.WriteString(hLine("┌", "┬", "┐") + "\n")
	sb.WriteString(rowLine(tableRow{}, true) + "\n") // header
	for _, r := range rows {
		sb.WriteString(hLine("├", "┼", "┤") + "\n")
		sb.WriteString(rowLine(r, false) + "\n")
	}
	sb.WriteString(hLine("└", "┴", "┘"))
	return sb.String()
}
