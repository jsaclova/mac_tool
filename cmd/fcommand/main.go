// fcommand - 듀얼패널 파일관리자 (단독 실행)
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"fm/internal/dualpane"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	args := os.Args[1:]
	if len(args) >= 1 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("사용: fcommand [좌경로] [우경로]")
		fmt.Println("키: Tab 전환 · Enter 진입 · ← 상위 · a 숨김 · v 체크박스 · Space 선택 · y 복사 · p 붙이기 · m 이동 · Del 삭제 · s SSH접속(오른쪽) · b 접속해제·원복 · e 외장디스크 · esc 취소 · q 종료")
		return
	}
	home, _ := os.UserHomeDir()
	left, right := home, home
	if len(args) >= 1 {
		left = args[0]
	}
	if len(args) >= 2 {
		right = args[1]
	}
	absL, _ := filepath.Abs(left)
	absR, _ := filepath.Abs(right)
	p := tea.NewProgram(dualpane.New(absL, absR))
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "실행 실패:", err)
		os.Exit(1)
	}
}
