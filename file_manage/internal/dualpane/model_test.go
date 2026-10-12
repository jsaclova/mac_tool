package dualpane

import "testing"

// sameLocation은 원본 경로와 목적지 경로가 같으면 true여야 한다.
// 자기복사(^V) 시 파일 유실 방지의 핵심이므로 회귀 테스트로 고정한다.
func TestSameLocationSelfCopy(t *testing.T) {
	local := LocalFS{}
	cases := []struct {
		name   string
		srcFS  FS
		src    string
		dstFS  FS
		dstDir string
		want   bool
	}{
		{
			name:  "로컬 폴더 자기복사 (같은 디렉터리에 붙여넣기)",
			srcFS: local, src: "/a/sub",
			dstFS: local, dstDir: "/a",
			want: true,
		},
		{
			name:  "로컬 파일 자기복사 (같은 디렉터리에 붙여넣기)",
			srcFS: local, src: "/a/file.txt",
			dstFS: local, dstDir: "/a",
			want: true,
		},
		{
			name:  "로컬 파일/폴더 다른 곳으로 복사",
			srcFS: local, src: "/a/sub",
			dstFS: local, dstDir: "/c",
			want: false,
		},
		{
			name:  "다른 FS (로컬 → SFTP)",
			srcFS: local, src: "/a/sub",
			dstFS: &SftpFS{}, dstDir: "/a",
			want: false,
		},
		{
			name:  "원격 동일 경로",
			srcFS: &SftpFS{}, src: "/a/sub",
			dstFS: &SftpFS{}, dstDir: "/a",
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameLocation(tc.srcFS, tc.src, tc.dstFS, tc.dstDir); got != tc.want {
				t.Errorf("sameLocation(srcFS=%T, %q, dstFS=%T, %q) = %v, want %v",
					tc.srcFS, tc.src, tc.dstFS, tc.dstDir, got, tc.want)
			}
		})
	}
}
