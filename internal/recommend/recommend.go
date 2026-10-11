package recommend

import (
	"path/filepath"
	"sort"
	"strings"

	"fm/internal/scanner"
)

type Rec struct {
	Path   string
	Size   int64 // 실제 점유
	Reason string
	Kind   Kind
}

type Kind int

const (
	Normal Kind = iota
	Delete
	Backup
)

// 백업/삭제 후보 확장자·경로 규칙
var backupExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".heic": true, ".raw": true,
	".psd": true, ".ai": true, ".pdf": true, ".docx": true, ".xlsx": true,
	".pptx": true, ".mov": true, ".mp4": true, ".mkv": true, ".wav": true,
	".zip": true, ".sqlite": true, ".db": true,
}

var installerExts = map[string]bool{
	".iso": true, ".dmg": true, ".pkg": true, ".zip": true, ".tar": true, ".gz": true,
}

var cacheMarkers = []string{
	"Library/Caches", ".cache", "__pycache__", "node_modules", ".npm",
	".Trash", "/tmp/", ".log", "Chrome/OptGuideOnDeviceModel",
}

var backupDirMarkers = []string{
	"Pictures", "Movies", "Documents", "Desktop", "work", "project",
	"iCloud", "MYBOX", "linux",
}

// 소스·문서 등 텍스트 기반 가치 확장자 (백업 디렉토리 안에 있을 때만 후보)
var valuableExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".ts": true, ".md": true,
	".txt": true, ".json": true, ".sh": true, ".yaml": true, ".yml": true,
}

// 재생성 가능 파일 (어느 목록에도 넣지 않음)
var regenerableNames = map[string]bool{
	"go.sum": true, "go.mod": true,
}

func isRegenerable(path string) bool {
	base := filepath.Base(path)
	if regenerableNames[base] {
		return true
	}
	if strings.Contains(path, "/.git/") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".o", ".a", ".test", ".pyc":
		return true
	}
	return false
}

func containsAny(path string, markers []string) string {
	for _, m := range markers {
		if strings.Contains(path, m) {
			return m
		}
	}
	return ""
}

// ClassifyFile는 단일 경로를 삭제/백업/일반으로 분류한다.
func ClassifyFile(path string, size int64) (Kind, string) {
	ext := strings.ToLower(filepath.Ext(path))

	if mark := containsAny(path, cacheMarkers); mark != "" {
		reason := "캐시/재생성 가능 (" + mark + ")"
		if strings.Contains(path, "OptGuideOnDeviceModel") {
			reason = "Chrome 온디바이스 모델 — 재생성 가능"
		}
		if strings.HasSuffix(path, ".vmem") {
			reason = "VM 절전 메모리 — VM 종료 후 삭제 가능"
		}
		return Delete, reason
	}
	if strings.HasSuffix(path, "Docker.raw") {
		return Delete, "Docker VM 디스크 — 직접삭제 금지, docker system prune으로 정리"
	}
	if strings.HasSuffix(path, ".vmem") {
		return Delete, "VM 절전 메모리 — VM 종료 후 삭제 가능"
	}
	if strings.Contains(path, ".ollama/models/blobs/") {
		return Delete, "Ollama 모델 — 미사용이면 ollama rm"
	}
	if strings.HasSuffix(path, ".vmdk") {
		return Delete, "VM 가상디스크 — 미사용 VM이면 통째로 삭제 검토"
	}
	if installerExts[ext] {
		return Delete, "설치 이미지/압축 — 설치 후 불필요하면 삭제"
	}
	if isRegenerable(path) {
		return Normal, ""
	}
	if backupExts[ext] {
		return Backup, "미디어/문서 — 외장디스크 백업 권장 (c=복사)"
	}
	if valuableExts[ext] && containsAny(path, backupDirMarkers) != "" {
		return Backup, "작업 소스/문서 — 백업 권장 (c=복사)"
	}
	if size >= 1<<30 {
		return Backup, "1GB 이상 대용량 — 백업 후 삭제 검토"
	}
	return Normal, ""
}

// Recommend는 실제 점유 기준 TopFiles/DirSizes에서 백업·삭제 후보를 뽑는다.
func Recommend(files []scanner.FileEntry, dirs []scanner.FileEntry) (backup, del []Rec) {
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f.Path] {
			continue
		}
		seen[f.Path] = true
		kind, reason := ClassifyFile(f.Path, f.Size)
		switch kind {
		case Delete:
			del = append(del, Rec{f.Path, f.Size, reason, Delete})
		case Backup:
			backup = append(backup, Rec{f.Path, f.Size, reason, Backup})
		}
	}
	// 폴더 단위 힌트
	for _, d := range dirs {
		base := filepath.Base(d.Path)
		switch base {
		case "node_modules", "__pycache__", ".npm", ".cache", "Caches":
			del = append(del, Rec{d.Path, d.Size, "재생성 가능 폴더 — 삭제 안전", Delete})
		case "Virtual Machines.localized":
			del = append(del, Rec{d.Path, d.Size, "VM 모음 — 미사용 VM 정리", Delete})
		}
	}

	sort.Slice(backup, func(i, j int) bool { return backup[i].Size > backup[j].Size })
	sort.Slice(del, func(i, j int) bool { return del[i].Size > del[j].Size })
	return backup, del
}
