package main

import (
	"bytes"
	"io"
	"reflect"
	"testing"
	"time"
)

func Test_parseVideoTimestamp(t *testing.T) {
	type args struct {
		tsStr string
	}
	tests := []struct {
		name    string
		args    args
		want    uint64
		want1   time.Duration
		wantErr bool
	}{
		{
			name: "foo",
			args: args{
				tsStr: "3,4:54",
			},
			want:    3,
			want1:   duration(4, 54),
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := parseVideoTimestamp(tt.args.tsStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseVideoTimestamp() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseVideoTimestamp() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("parseVideoTimestamp() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

type withClose struct {
	io.Writer
}

func (wc *withClose) Close() error { return nil }

func bufferWriteCloser(buf *bytes.Buffer) func(string) (io.WriteCloser, error) {
	return func(_ string) (io.WriteCloser, error) {
		return &withClose{buf}, nil
	}
}

const (
	file0 = `file 038/output000.mp4
inpoint 00:00:10

file 038/output001.mp4

file 038/output002.mp4

file 038/output003.mp4

file 038/output004.mp4

file 038/output005.mp4
`
	file1 = `file 038/output006.mp4

file 038/output007.mp4

file 038/output008.mp4

file 038/output009.mp4

file 038/output010.mp4

file 038/output011.mp4
`
	file2 = `file 038/output012.mp4

file 038/output013.mp4

file 038/output014.mp4

file 038/output015.mp4

file 038/output016.mp4

file 038/output017.mp4
`
	file3 = `file 038/output018.mp4

file 038/output019.mp4

file 038/output020.mp4
outpoint 00:09:30
`
)

func Test_makeVideoConfig(t *testing.T) {
	cfg := &Config{
		InputDir:     "038",
		FromVideo:    0,
		FromDuration: 10 * time.Second,
		ToVideo:      20,
		ToDuration:   (9*60 + 30) * time.Second,
		PerVideo:     6,
	}
	tests := []struct {
		name       string
		cfg        *Config
		videoIndex uint64
		wantErr    bool
		wantFile   string
	}{
		{
			name:       "0",
			cfg:        cfg,
			videoIndex: 0,
			wantFile:   file0,
		},
		{
			name:       "1",
			cfg:        cfg,
			videoIndex: 1,
			wantFile:   file1,
		},
		{
			name:       "2",
			cfg:        cfg,
			videoIndex: 2,
			wantFile:   file2,
		},
		{
			name:       "3",
			cfg:        cfg,
			videoIndex: 3,
			wantFile:   file3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := new(bytes.Buffer)
			if err := makeVideoConfig(tt.cfg, tt.videoIndex, bufferWriteCloser(buf), ""); (err != nil) != tt.wantErr {
				t.Errorf("makeVideoConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			got := buf.String()
			if got != tt.wantFile {
				t.Errorf("makeVideoConfig() got = %q, want = %q", got, tt.wantFile)
			}
		})
	}
}
