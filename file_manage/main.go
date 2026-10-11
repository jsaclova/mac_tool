package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	copyx "fm/internal/copyx"
	"fm/internal/dualpane"
	"fm/internal/human"
	"fm/internal/recommend"
	"fm/internal/scanner"
	"fm/internal/ui"
	"fm/internal/volumes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

// systemRoot는 전체 시스템 실데이터 루트를 반환한다.
func systemRoot() string {
	if runtime.GOOS == "darwin" {
		return "/System/Volumes/Data"
	}
	return "/"
}

func main() {
	var topN int
	var minSize string
	var noExclude bool
	var copyDest string

	root := &cobra.Command{
		Use:   "fm [경로]",
		Short: "빠른 로컬 파일시스템 관리자 (기본: 전체 시스템)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := systemRoot()
			if len(args) == 1 {
				path = args[0]
			}
			abs, _ := filepath.Abs(path)
			p := tea.NewProgram(ui.New(abs))
			_, err := p.Run()
			return err
		},
	}

	analyze := &cobra.Command{
		Use:   "analyze [경로]",
		Short: "대용량 파일/폴더 분석 (非TUI, 기본: 전체 시스템)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := systemRoot()
			if len(args) == 1 {
				path = args[0]
			}
			abs, _ := filepath.Abs(path)
			var excl []string
			if !noExclude {
				excl = []string{".Trash", "Library/Caches", ".cache", ".npm"}
			}
			var res scanner.Result
			for ev := range scanner.ScanStream(abs, topN, human.Parse(minSize), excl) {
				if ev.Done {
					res = ev.Result
				} else {
					fmt.Fprintf(os.Stderr, "\r스캔 중… %d 파일 · %s", ev.Files, human.Size(ev.Bytes))
				}
			}
			fmt.Fprintln(os.Stderr)
			fmt.Printf("스캔: %s (파일 %d개, 실제점유 %s, 에러 %d) ※실제 디스크 점유 기준\n\n", abs, res.TotalFiles, human.Size(res.TotalBytes), res.Errors)
			fmt.Printf("%4s  %10s  %s\n", "순위", "실제크기", "경로")
			fmt.Println("--------------------------------------------------------------------------------")
			for i, e := range res.TopFiles {
				kind, _ := recommend.ClassifyFile(e.Path, e.Size)
				tag := "  "
				if kind == recommend.Delete {
					tag = "🗑 "
				} else if kind == recommend.Backup {
					tag = "💾 "
				}
				fmt.Printf("%4d  %10s  %s%s\n", i+1, human.Size(e.Size), tag, e.Path)
				if i+1 >= topN {
					break
				}
			}
			fmt.Printf("\n---직속 폴더 합산 Top %d (실제점유)---\n", topN)
			for i, e := range res.DirSizes {
				fmt.Printf("%4d  %10s  %s\n", i+1, human.Size(e.Size), e.Path)
				if i+1 >= topN {
					break
				}
			}
			backup, del := recommend.Recommend(res.TopFiles, res.DirSizes)
			fmt.Printf("\n=== 🗑 삭제 후보 Top %d ===\n", min(10, len(del)))
			if len(del) == 0 {
				fmt.Println("(없음)")
			}
			var delSum int64
			for i, r := range del {
				if i >= 10 {
					break
				}
				fmt.Printf("%10s  %s\n  └ %s\n", human.Size(r.Size), r.Path, r.Reason)
				delSum += r.Size
			}
			if len(del) > 0 {
				fmt.Printf("→ 삭제 후보 합계(상위 10): %s\n", human.Size(delSum))
			}
			fmt.Printf("\n=== 💾 백업 후보 Top %d ===\n", min(10, len(backup)))
			if len(backup) == 0 {
				fmt.Println("(없음)")
			}
			var bakSum int64
			for i, r := range backup {
				if i >= 10 {
					break
				}
				fmt.Printf("%10s  %s\n  └ %s\n", human.Size(r.Size), r.Path, r.Reason)
				bakSum += r.Size
			}
			if len(backup) > 0 {
				fmt.Printf("→ 백업 후보 합계(상위 10): %s\n", human.Size(bakSum))
			}
			return nil
		},
	}
	analyze.Flags().IntVarP(&topN, "top", "n", 20, "출력 개수")
	analyze.Flags().StringVar(&minSize, "min-size", "100MB", "최소 파일 크기")
	analyze.Flags().BoolVar(&noExclude, "include-caches", false, "캐시 포함")

	copyCmd := &cobra.Command{
		Use:   "copy <원본...> --to <외장디스크/폴더>",
		Short: "백업 대상을 외장디스크에 복사",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if copyDest == "" {
				return fmt.Errorf("--to 로 복사 대상 폴더를 지정하세요 (예: --to /Volumes/Backup)")
			}
			if st, err := os.Stat(copyDest); err != nil || !st.IsDir() {
				return fmt.Errorf("대상 폴더 없음: %s (외장디스크 마운트 확인)", copyDest)
			}
			var totalF, totalB int64
			for _, src := range args {
				f, b, err := copyx.CopyInto(src, copyDest, nil)
				if err != nil {
					fmt.Printf("스킵 %s: %v\n", src, err)
					continue
				}
				fmt.Printf("복사 %s → %s (%s)\n", src, copyDest, human.Size(b))
				totalF += f
				totalB += b
			}
			fmt.Printf("완료: %d개 항목, %s\n", totalF, human.Size(totalB))
			return nil
		},
	}
	copyCmd.Flags().StringVar(&copyDest, "to", "", "복사 대상 폴더 (외장디스크 경로)")

	volsCmd := &cobra.Command{
		Use:   "volumes",
		Short: "외장디스크 목록",
		RunE: func(cmd *cobra.Command, args []string) error {
			vols := volumes.ListExternal()
			if len(vols) == 0 {
				fmt.Println("외장디스크 없음")
				return nil
			}
			for _, v := range vols {
				fmt.Println(v)
			}
			return nil
		},
	}

	dualCmd := &cobra.Command{
		Use:   "dual [좌경로] [우경로]",
		Short: "듀얼패널 파일관리자 (좌우 복사/이동/삭제, SSH)",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			_, err := p.Run()
			return err
		},
	}

	root.AddCommand(analyze, copyCmd, volsCmd, dualCmd)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
