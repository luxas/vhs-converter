package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/artdarek/go-unzip"
	"github.com/otiai10/copy"
	"github.com/spf13/pflag"
)

const (
	ffmpegBinaryName = "ffmpeg.exe"
	ffmpegZipName    = "ffmpeg.zip"
	ffmpegExtractDir = "ffmpeg/"
	ffmpegBuildName  = "ffmpeg-4.3-win64-static"
	ffmpegURL        = "https://ffmpeg.zeranoe.com/builds/win64/static/" + ffmpegBuildName + ".zip"
	unknownMark      = "<unknown>"
)

var (
	version = unknownMark
	commit  = unknownMark
	date    = unknownMark
)

type Config struct {
	PerVideo  uint64
	From      string // e.g. 0,3m18s
	To        string // e.g. 13,4m22s
	InputDir  string // 038
	OutputDir string // 038/merged
	Force     bool
	FixAudio  bool

	FromVideo    uint64
	FromDuration time.Duration
	ToVideo      uint64
	ToDuration   time.Duration
	ClipCount    uint64
}

func (c *Config) RegisterFlags(fs *pflag.FlagSet) {
	fs.StringVar(&c.From, "from", c.From, "Foo")
	fs.StringVar(&c.To, "to", c.To, "bar")
	fs.Uint64VarP(&c.PerVideo, "per-clip", "n", c.PerVideo, "baz")
	fs.StringVarP(&c.InputDir, "in-dir", "i", c.InputDir, "foo")
	fs.StringVarP(&c.OutputDir, "out-dir", "o", c.OutputDir, "foo")
	fs.BoolVarP(&c.Force, "force", "f", c.Force, "whether to overwrite possible files")
	fs.BoolVar(&c.FixAudio, "fix-audio", c.FixAudio, "whether to fix audio to use aac or not")
}

func (c *Config) Complete() (err error) {
	c.FromVideo, c.FromDuration, err = parseVideoTimestamp(c.From)
	if err != nil {
		return
	}

	c.ToVideo, c.ToDuration, err = parseVideoTimestamp(c.To)
	if err != nil {
		return
	}

	c.ClipCount = c.ToVideo - c.FromVideo + 1
	if c.PerVideo == 0 {
		c.PerVideo = c.ClipCount
	} else if c.ClipCount < c.PerVideo {
		return fmt.Errorf("specified amount of videos are %d, can't split in %d parts", c.ClipCount, c.PerVideo)
	}

	if c.OutputDir == "" {
		c.OutputDir = filepath.Join(c.InputDir, "merged")
	}

	return
}

const videoTimestampPattern = `([0-9]+),([0-9]{1,2}):([0-9]{1,2})`

var fooRegex = regexp.MustCompile(videoTimestampPattern)

func parseVideoTimestamp(tsStr string) (uint64, time.Duration, error) {
	if len(tsStr) == 0 {
		return 0, 0, fmt.Errorf("timestamp string is required")
	}

	match := fooRegex.FindAllStringSubmatch(tsStr, -1)
	parts := match[0][1:]
	if len(parts) != 3 || len(parts[0]) == 0 || len(parts[1]) == 0 || len(parts[2]) == 0 {
		return 0, 0, fmt.Errorf("invalid timestamp format, should be like 3,5:32")
	}

	numParts := make([]uint64, len(parts))
	var err error
	for i := range parts {
		numParts[i], err = strconv.ParseUint(parts[i], 10, 64)
		if err != nil {
			return 0, 0, err
		}
	}

	return numParts[0], duration(numParts[1], numParts[2]), nil
}

func duration(min, sec uint64) time.Duration {
	return time.Duration(min*uint64(time.Minute) + sec*uint64(time.Second))
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if version != unknownMark {
		fmt.Printf("Running vhs-converter %s (commit: %s, build date: %s)\n", version, commit, date)
	}

	c := Config{
		FixAudio: true,
	}
	c.RegisterFlags(pflag.CommandLine)
	pflag.Parse()
	if err := c.Complete(); err != nil {
		pflag.Usage()
		return err
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return err
	}

	ffmpegBinary := filepath.Join(cacheDir, ffmpegBinaryName)

	if err := install(ffmpegBinary); err != nil {
		return err
	}

	return start(&c, ffmpegBinary)
}

func newWriteCloser(fileName string) (io.WriteCloser, error) {
	return os.Create(fileName)
}

func start(c *Config, ffmpegExecutable string) error {
	videoCount := uint64(math.Ceil(float64(c.ClipCount) / float64(c.PerVideo)))
	for i := uint64(0); i < videoCount; i++ {
		outFile := filepath.Join(c.OutputDir, fmt.Sprintf("%03d.mp4", i))
		if err := os.MkdirAll(filepath.Dir(outFile), 0755); err != nil {
			return err
		}
		// Make sure we don't overwrite files unnecessary
		if fileExists(outFile) && !c.Force {
			return fmt.Errorf("file %s already exists, won't overwrite unless -f is applied", outFile)
		}

		cfgFile := fmt.Sprintf("%s.txt", outFile)
		if err := makeVideoConfig(c, i, newWriteCloser, cfgFile); err != nil {
			return err
		}

		args := []string{"-f", "concat", "-i", cfgFile, "-c:v", "copy"}
		if c.Force {
			args = append(args, "-y")
		}
		if c.FixAudio {
			args = append(args, "-c:a", "aac")
		} else {
			args = append(args, "-c:a", "copy")
		}
		args = append(args, outFile)
		if err := executeCommand(ffmpegExecutable, args...); err != nil {
			return err
		}
		if err := os.RemoveAll(cfgFile); err != nil {
			return err
		}
	}
	return nil
}

func makeVideoConfig(c *Config, videoIndex uint64, writeCloserFunc func(string) (io.WriteCloser, error), cfgFile string) error {
	f, err := writeCloserFunc(cfgFile)
	if err != nil {
		return err
	}
	defer f.Close()
	log.Printf("Creating ffmpeg configuration file...")
	w := io.MultiWriter(os.Stdout, f)

	for i := uint64(0); i < c.PerVideo; i++ {
		j := videoIndex*c.PerVideo + i
		fmt.Fprintf(w, "file %s\n", path.Join(c.InputDir, fmt.Sprintf("output%03d.mp4", j)))
		if j == 0 {
			mins := int(c.FromDuration.Seconds() / 60)
			secs := int(int(c.FromDuration.Seconds()) - mins*60)
			fmt.Fprintf(w, "inpoint 00:%02d:%02d\n", mins, secs)
		} else if j == c.ToVideo {
			mins := int(c.ToDuration.Seconds() / 60)
			secs := int(int(c.ToDuration.Seconds()) - mins*60)
			fmt.Fprintf(w, "outpoint 00:%02d:%02d\n", mins, secs)
		}
		if i == c.PerVideo-1 || j == c.ToVideo {
			break
		}
		fmt.Fprint(w, "\n")
	}

	return nil
}

func install(dest string) error {
	if fileExists(dest) {
		log.Printf("ffmpeg binary %s already exists...", dest)
		return nil
	}

	log.Printf("Installing ffmpeg into %s...", dest)
	tmpDir := filepath.Join(os.TempDir(), ffmpegExtractDir)
	log.Printf("Ensuring directory %s exists...", tmpDir)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	zipPath := filepath.Join(tmpDir, ffmpegZipName)
	extractPath := filepath.Join(tmpDir, ffmpegExtractDir)

	if err := downloadFFmpegZip(zipPath); err != nil {
		return err
	}
	if err := extractZipFile(zipPath, extractPath); err != nil {
		return err
	}

	from := filepath.Join(extractPath, ffmpegBuildName, "bin", ffmpegBinaryName)

	log.Printf("Copying %s to %s...", from, dest)
	return copy.Copy(from, dest)
}

func downloadFFmpegZip(zipPath string) error {
	// Exit fast if possible
	if fileExists(zipPath) {
		log.Printf("File %s already exists...", zipPath)
		return nil
	}

	log.Printf("Downloading %q into %s...", ffmpegURL, zipPath)

	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	resp, err := http.Get(ffmpegURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(f, resp.Body)
	if err == nil {
		log.Printf("Download successful: %t", err == nil)
	} else {
		log.Printf("Failed downloading: %v", err)
	}
	return err
}

func extractZipFile(zipPath, extractPath string) error {
	if fileExists(extractPath) {
		log.Printf("Directory %s already exists...", extractPath)
		return nil
	}

	log.Printf("Extracting %q into %s...", zipPath, extractPath)
	err := unzip.New(zipPath, extractPath).Extract()
	log.Printf("Extract successful: %t", err == nil)
	return err
}

func fileExists(file string) bool {
	_, err := os.Stat(file)
	return !os.IsNotExist(err)
}

func executeCommand(binary string, args ...string) error {
	log.Printf("Executing: %q %v...", binary, args)
	cmd := exec.Command(binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
