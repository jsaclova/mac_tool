# fm — 빠른 로컬 파일시스템 터미널 관리자

Go + BubbleTea 기반 디스크 용량 분석/탐색 TUI. `st_blocks` 실제 점유 기준, 병렬 스캔.

## 실행

```bash
fm              # 전체 시스템 TUI (macOS: /System/Volumes/Data)
fm [경로]       # 지정 경로 TUI
fm dual [좌] [우]  # 듀얼패널 (좌우 복사/이동/삭제, SSH)
fm analyze [경로] -n 20 --min-size 100MB   # 비대화식 분석
fm copy <원본...> --to <외장경로>          # 외장디스크 복사
fm volumes      # 외장디스크 목록
```

전역 설치: `go build -o fm . && cp fm /usr/local/bin/fm`

## TUI 키 (Analyze)

`↑↓/j/k` 이동 · `Enter` 하위 · `←/h/Backspace` 상위 · `q` 종료

## 듀얼패널 키 (`fm dual`)

`Tab` 전환 · `↑↓` 이동 · `Enter` 진입 · `←` 상위 · `c` 복사→ · `m` 이동→ · `Del` 삭제(확인) · `s` 주소연결 · `q` 종료

- 주소창: `user@host:/경로`, `ssh://user@host:port/경로`, 로컬 경로. 입력 기록은 `~/.config/fm/hosts.json` 캐시 (↑↓ 선택)
- 인증: ssh-agent → `~/.ssh` 키 → 비밀번호 입력 순. 호스트키는 `~/.ssh/known_hosts` TOFU 등록
- 로컬 삭제는 휴지통 이동, 원격 삭제는 즉시 삭제

## 구조

```
main.go                    cobra CLI (fm/analyze/copy/volumes/dual)
internal/scanner/          병렬 스캔 (세마포어 고루틴 + 단일 집계기 + Top-N 힙)
internal/ui/               BubbleTea TUI (dua풍 바 차트, 박스 고정 레이아웃)
internal/dualpane/         듀얼패널 (FS 추상화 + SFTP + SSH 주소캐시 + 좌우 복사/이동/삭제)
internal/human/            GB/MB 표시, 크기 파싱
internal/recommend/        백업/삭제 분류 (CLI analyze 표시용)
internal/copyx/            외장 복사, internal/volumes/ 외장 감지, internal/trash/ 휴지통
find_large_*.py            초기 프로토타입 (참고용)
```

## 핵심 결정

- 실제 점유(`st_blocks×512`) 기준. Docker.raw 같은 sparse 과대 표시 방지
- 동일 볼륨만 탐색 (외장디스크 제외, 합계 정확)
- 하드링크 중복 집계 제외 (dev,ino)
- 상위 복귀·재방문은 메모리 스냅샷 캐시 (재스캔 없음), 낡은 스캔은 세대번호로 폐기
- 제외 규칙 없음 (첫 스캔=재스캔 동일 → 폴더 이동 시 합계 일치)
- 고정 전체화면 레이아웃 (헤더 3줄+목록+푸터, 터미널 높이와 항상 일치)

## 성능

- 홈 39만 파일 3.9초, 전체 174만 파일 19초 (`du` 대비 약 3배)
- 병목은 파일당 `lstat` 시스템콜 (하한선)

## 주의

- APFS 클론 공유 블록은 중복 집계될 수 있어 `df`보다 약 8% 크게 나옴
- 세션 캐시는 무효화 없음 (외부 변경 미반영)
