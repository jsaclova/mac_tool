# file_manage 개발 요약

빠른 로컬 파일시스템/원격(SFTP) 터미널 관리자. Go + BubbleTea.
디스크 용량 분석(`fm`)과 듀얼패널 파일관리자(`fcommand`) 두 실행 파일을 제공한다.

## 실행 파일

| 바이너리 | 설명 |
| --- | --- |
| `fm` | 전체 시스템/경로 디스크 사용량 TUI, `analyze`/`copy`/`volumes` 서브커맨드 |
| `fcommand` | 좌/우 듀얼패널 파일관리자 (복사/이동/삭제, SSH/SFTP, 외장디스크) |

```bash
fm                    # 전체 시스템 TUI (macOS: /System/Volumes/Data)
fm [경로]             # 지정 경로 TUI
fcommand [좌] [우]    # 듀얼패널
fm analyze [경로] -n 20 --min-size 100MB
fm copy <원본...> --to <외장경로>
fm volumes            # 외장디스크 목록
```

## 듀얼패널 키 (`fcommand`)

`Tab` 전환 · `↑↓` 이동 · `Enter` 진입 · `←` 상위 · `a` 숨김파일 · `v` 체크박스 ·
`Space` 멀티선택 · `y` 복사(yank) · `p` 붙여넣기 · `m` 반대편으로 이동 · `Del` 삭제(확인) ·
`s` SSH 접속(오른쪽) · `b` 접속해제·원복 · `e` 외장디스크 · `esc` 취소 · `q` 종료

## 주요 기능 (듀얼패널)

- **복사/이동**: `y`(yank)로 대상 지정 → 반대 패널에서 `p`(붙여넣기), `m`(반대편으로 이동).
  같은 위치로의 자기복사는 건너뛰어 0바이트 덮어쓰기 데이터 유실을 방지한다.
- **상태바**: 복사 대기 목록(파일명)과 실시간 진행상황(항목 n/N · 파일 수 · 용량)을 하단에 표시.
  고루틴 + 채널로 진행 메시지를 스트리밍한다.
- **작업 후 원복**: 복사/이동 완료 시 초록 yank 하이라이트와 체크박스(marks)를 자동 해제.
- **SSH 접속 폼(`s`)**: IP/호스트 · 아이디 · 비밀번호를 입력. `Tab`(또는 `↑↓`)으로 필드 이동,
  `Enter`로 연결. 접속은 활성 패널과 무관하게 **항상 오른쪽 패널**. 비밀번호는 `•`로 마스킹.
  호스트에 `/경로`·`~/경로`를 넣으면 오른쪽이 로컬 경로로 전환.
- **접속해제·원복(`b`)**: 패널별 백업(`backup[2]`)으로 SSH/외장 연결 직전 상태를 저장해 두고,
  `b`로 되돌린다. 활성 패널에 되돌릴 것이 없으면 오른쪽 SSH를 해제.
- **외장디스크(`e`)**: `volumes.ListExternal()`로 감지한 볼륨 목록을 중앙 모달 메뉴로 띄우고,
  선택 시 **오른쪽 패널**로 연다 (연결 후 `b`로 원복).
- **인증**: ssh-agent → `~/.ssh` 키 → 비밀번호(`password` + `keyboard-interactive` 폴백) 순.
  호스트키는 `~/.ssh/known_hosts` TOFU 자동 등록. 오류는 한국어 안내로 변환.
- 로컬 삭제는 휴지통 이동, 원격 삭제는 즉시 삭제.

## 구조

```
main.go                    # fm (용량 분석/복사/볼륨) CLI
cmd/fcommand/main.go       # fcommand 듀얼패널 단독 실행
internal/dualpane/         # 듀얼패널 TUI (model.go, ops.go, sftpfs.go, hosts.go + 테스트)
internal/human/            # 용량 포맷
internal/scanner/          # 디스크 스캔
internal/recommend/        # 정리 추천
internal/trash/            # 휴지통
internal/volumes/          # 외장 볼륨 목록
internal/ui/               # 공통 TUI
```

## 테스트

`go test ./...` — `internal/dualpane` 중심:

- `model_test.go` : `sameLocation` (자기복사 판별 5케이스)
- `paste_test.go` : `TestPasteSelfCopySkipped`, `TestPasteToOtherPane`, `TestMoveResetsState`
- `layout_test.go` : `TestViewFixedHeight` (복사대기 줄 유무와 무관하게 전체 높이 고정)
- `connect_test.go` : 키 재바인딩, 접속 폼 필드/마스킹/로컬전환, 접속해제 원복, 외장디스크 메뉴/원복

## 최근 변경

- 커밋 `6a88217` — fcommand: yank/paste, 상태바, SSH 폼, 접속해제·외장디스크
- 키 재바인딩: `^C`/`^V`/`^M`(터미널 제어문자와 충돌) → `y`/`p`/`m`
- `cmd/fc` → `cmd/fcommand` 이름 변경

## 알려진 제약

- 실터미널의 키 전달은 이 개발 환경(pty 하네스)에서 검증 불가 — 실제 터미널에서 확인 필요.
- 비밀번호 인증은 서버 정책에 따라 `password` 또는 `keyboard-interactive` 만 허용될 수 있음(둘 다 시도).
