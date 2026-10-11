#!/usr/bin/env python3
"""용량을 많이 차지하는 파일 Top-N 리스트 출력 스크립트 (macOS/Linux/Windows)."""
import argparse
import os
import sys
from pathlib import Path

DEFAULT_EXCLUDES = {".Trash", "Library/Caches", ".cache", ".npm", ".docker"}

def human_size(n: int) -> str:
    for unit in ["B", "KB", "MB", "GB", "TB"]:
        if n < 1024 or unit == "TB":
            return f"{n:.1f}{unit}" if unit != "B" else f"{n}B"
        n /= 1024
    return f"{n:.1f}TB"

def should_exclude(path: Path, excludes: set[str], root: Path) -> bool:
    try:
        rel = str(path.relative_to(root))
    except ValueError:
        rel = str(path)
    return any(rel.startswith(e) or f"/{e}/" in f"/{rel}/" or rel == e for e in excludes)

def scan_large_files(root: Path, top_n: int, min_size: int, excludes: set[str]):
    results: list[tuple[int, str]] = []
    scanned = 0
    errors = 0
    stack = [root]
    while stack:
        cur = stack.pop()
        try:
            with os.scandir(cur) as it:
                for entry in it:
                    try:
                        if entry.is_symlink():
                            continue
                        if entry.is_dir(follow_symlinks=False):
                            p = Path(entry.path)
                            if should_exclude(p, excludes, root):
                                continue
                            stack.append(p)
                        elif entry.is_file(follow_symlinks=False):
                            scanned += 1
                            size = entry.stat(follow_symlinks=False).st_size
                            if size >= min_size:
                                results.append((size, entry.path))
                    except (PermissionError, FileNotFoundError, OSError):
                        errors += 1
        except (PermissionError, FileNotFoundError, OSError):
            errors += 1
    results.sort(key=lambda x: x[0], reverse=True)
    return results[:top_n], scanned, errors

def parse_size(s: str) -> int:
    s = s.strip().upper()
    mult = {"B": 1, "KB": 1024, "MB": 1024**2, "GB": 1024**3, "TB": 1024**4}
    for suffix in ["TB", "GB", "MB", "KB", "B"]:
        if s.endswith(suffix):
            return int(float(s[: -len(suffix)] or 0) * mult[suffix])
    return int(float(s))

def main():
    ap = argparse.ArgumentParser(description="용량 큰 파일 Top-N 조회")
    ap.add_argument("path", nargs="?", default=str(Path.home()), help="스캔 시작 경로 (기본: 홈)")
    ap.add_argument("-n", "--top", type=int, default=20, help="출력 개수 (기본: 20)")
    ap.add_argument("--min-size", default="100MB", help="최소 파일 크기 (예: 10MB, 1GB. 기본: 100MB)")
    ap.add_argument("--exclude", action="append", default=[], help="제외 경로(상대경로, 반복 가능)")
    ap.add_argument("--include-caches", action="store_true", help="캐시 폴더도 포함")
    args = ap.parse_args()

    root = Path(args.path).expanduser().resolve()
    if not root.exists():
        print(f"경로 없음: {root}", file=sys.stderr)
        sys.exit(1)

    excludes = set(args.exclude)
    if not args.include_caches:
        excludes |= DEFAULT_EXCLUDES

    min_size = parse_size(args.min_size)
    print(f"스캔 중: {root} (최소 {human_size(min_size)}, Top {args.top})...")
    top, scanned, errors = scan_large_files(root, args.top, min_size, excludes)

    total = sum(s for s, _ in top)
    print(f"\n스캔 파일 수: {scanned:,}, 접근 실패: {errors}")
    print(f"Top {len(top)} 합계: {human_size(total)}\n")
    print(f"{'순위':>4}  {'크기':>10}  경로")
    print("-" * 80)
    for i, (size, path) in enumerate(top, 1):
        print(f"{i:>4}  {human_size(size):>10}  {path}")
    if not top:
        print("(조건에 맞는 파일 없음. --min-size 를 낮춰보세요)")

if __name__ == "__main__":
    main()
