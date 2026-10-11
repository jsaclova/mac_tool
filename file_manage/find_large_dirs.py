#!/usr/bin/env python3
"""전체 시스템 폴더 용량 Top-N 조회 (du 기반, macOS/Linux)."""
import argparse
import os
import subprocess
import sys
from pathlib import Path

def human_size(n: float) -> str:
    for unit in ["B", "KB", "MB", "GB", "TB"]:
        if n < 1024 or unit == "TB":
            return f"{n:.1f}{unit}" if unit != "B" else f"{int(n)}B"
        n /= 1024
    return f"{n:.1f}TB"

def du_1level(path: Path, timeout: int = 300) -> list[tuple[int, str]]:
    """주어진 경로 직속 자식들의 용량(bytes) 측정. du -sk 사용."""
    try:
        entries = [str(e.path) for e in os.scandir(path) if not e.is_symlink()]
    except (PermissionError, FileNotFoundError, OSError) as e:
        print(f"접근 불가: {path} ({e})", file=sys.stderr)
        return []
    if not entries:
        return []
    # du는 인자 수 제한 회피를 위해 나눠서 실행
    results = []
    chunk = 100
    for i in range(0, len(entries), chunk):
        cmd = ["du", "-sk"] + entries[i:i+chunk]
        try:
            out = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        except subprocess.TimeoutExpired:
            print(f"du 타임아웃: {path}", file=sys.stderr)
            continue
        for line in out.stdout.splitlines():
            try:
                kb, p = line.split("\t", 1)
                results.append((int(kb) * 1024, p))
            except ValueError:
                continue
    results.sort(key=lambda x: x[0], reverse=True)
    return results

def main():
    ap = argparse.ArgumentParser(description="폴더 용량 Top-N (직속 자식 기준)")
    ap.add_argument("path", nargs="?", default="/System/Volumes/Data",
                    help="스캔 경로 (기본: /System/Volumes/Data, macOS 실데이터)")
    ap.add_argument("-n", "--top", type=int, default=20)
    ap.add_argument("--depth", type=int, default=1,
                    help="1=직속 자식만, 2=상위 폴더도 내려가며 표시")
    args = ap.parse_args()

    root = Path(args.path).expanduser()
    print(f"측정 중: {root} (du -sk 직속 자식 기준)...")
    level1 = du_1level(root)
    if not level1:
        print("(결과 없음 — sudo 필요할 수 있음)")
        return

    print(f"\n[{root}] Top {min(args.top, len(level1))} 폴더/파일")
    print(f"{'순위':>4}  {'크기':>10}  경로")
    print("-" * 70)
    for i, (size, p) in enumerate(level1[:args.top], 1):
        print(f"{i:>4}  {human_size(size):>10}  {p}")

    if args.depth >= 2:
        print("\n--- 상위 5개 폴더 내부 breakdown ---")
        for size, p in level1[:5]:
            if not os.path.isdir(p):
                continue
            print(f"\n▶ {p} ({human_size(size)})")
            sub = du_1level(Path(p))[:5]
            for s2, p2 in sub:
                print(f"    {human_size(s2):>10}  {p2}")

if __name__ == "__main__":
    main()
