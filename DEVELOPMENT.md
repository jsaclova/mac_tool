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

`Tab` 전환 · `↑↓` 이동 · `Enter` 진입 · `←/h` 상위(연타로 `/`까지) · `/` 이동(`/` 루트 포함) ·
`?` 도움말 (`/h`·`/?` 입력 후 `Enter`도 동일, `Enter`로 닫기) · `q` 종료

## 주요 기능 (듀얼패널)

- **복사/이동**: `y`(yank)로 대상 지정 → 반대 패널에서 `p`(붙여넣기), `m`(반대편으로 이동).
  같은 위치로의 자기복사는 건너뛰어 0바이트 덮어쓰기 데이터 유실을 방지한다.
- **하단 분리 박스 2개**: 위 네비게이션과 같은 테두리 박스 형식. 왼쪽 `Clipboard`(요약 1줄+이름 최대 3줄) + 오른쪽 `Process`(상태 1줄+최근 기록 2줄+항목 수 1줄).
  고루틴 + 채널로 진행 메시지를 `Process`에 스트리밍하고, 완료 문구는 최대 2개까지 기록한다.
- **진행률(파일별)**: `FileProg{Files, Bytes, Name, FileTotal, FileDone}` 보고로 현재 파일 기준 `%`+바. 크기는 `ListDir`의 `Entry.Size`(낱파일은 `SizeOf`)에서 가져와 사전 탐색 없이 표시, 크기를 모르면 `%` 생략. `copyFile`이 1MB씩 읽으며 4MB마다 보고.
- **폭 정규화**: ko 로케일에서 runewidth가 `–`·`·`·`…`·`▶`·`→` 등을 2셀로 재서 박스를 넘치므로, `padW`·`padCell`·`shorten` 진입점에서 `disp()`로 ASCII 치환 후 측정. 탭·개행·CR도 공백으로 바꿔 행 분리/탭 점프를 막는다. 구조 문자는 ASCII만 사용.
- **빌드 태그**: `dualpane.BuildTag`를 `?` 도움말과 `fcommand -h`에 표시해 실행 버전 확인.
- **높이 맞춤**: 헤더 3 + 패널 3 + 푸터 6 = 12 크롬, 목록 줄 = 높이-12.
  줄 수가 높이를 초과하면 맨 윗줄이 잘리므로 `newlines == height-1`을 테스트로 고정.
  헤더·제목은 `fcommand` 접두사까지 계산해 1줄로 줄인다.
- **상위 이동**: `←`·`h`·`Backspace`는 항상 상위 폴더로 (연타하면 `/` 루트까지).
  `Enter`로 들어온 곳이면 히스토리도 함께 소모한다.
- **이동**: 같은 FS면 `rename`, 볼륨이 다르면(EXDEV) `trash`와 같은 방식으로 복사 후 원본 삭제. 진행률·덮어쓰기 팝업은 복사 경로를 그대로 탄다.
- **삭제 팝업**: `d`·`Del`로 대상 지정 후 중앙 팝업 "삭제하겠습니까?" (`y`·`Enter`·`d`=실행, `n`·`esc`=취소, 그 외 키 무시). 원격은 즉시 삭제 안내. 로컬은 `~/.Trash`로 이동(복구 가능), 외장디스크 등 다른 볼륨은 `rename` 불가라 복사 후 원본 삭제.
- **덮어쓰기 팝업**: 붙여넣기(`p`)·이동(`m`) 전 목적지 목록과 대조해 동명 항목이 있으면 "덮어쓰겠습니까?" 팝업 (`y`=모두 덮어쓰기, `s`=충돌만 건너뛰고 계속, `n`·`esc`=전체 취소). 원본과 동일 위치(runOp 건너뜀)는 제외. 맥 ⌫는 상위 이동 키이므로 `d`나 `fn+delete` 사용.- **상단·제목**: 헤더와 패널 제목은 시작 경로가 아닌 현재 위치만 표시한다.
- **이동(`/`)**: `/` 입력 시 상단 프롬프트(`/` 프리필)에 경로 입력 후 `Enter`.
  `/` 단독은 루트, `~`·상대경로 지원, 원격 패널은 원격 경로 기준.
- **도움말**: `?` 직접 표시, 또는 `/` 프롬프트에 `?`·`h`·`/?`·`/h` 입력 후 `Enter`.
  두줄 테두리 모달에 본문 2줄·강조 제목, `Enter`·`esc`로 닫기.
  하단 상시 키 설명은 표시하지 않는다.
- **폭 정렬**: 좌박스를 조금 줄여 좌(+테두리2)+구분자1+우(+테두리2) = 상·하단 폭에 정확히 맞춤.
  구조 문자(`>`·`...`·`|`·`_`)는 ASCII로 써서 터미널 셀 폭과 일치시킨다.
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
