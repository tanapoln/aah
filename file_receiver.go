// Copyright (c) Jeevanandam M (https://github.com/jeevatkm)
// go-aah/log source code and usage is governed by a MIT style
// license that can be found in the LICENSE file.

package log

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"aahframework.org/config.v0"
	"aahframework.org/essentials.v0"
)

var _ Receiver = &FileReceiver{}

// FileReceiver writes the log entry into file.
type FileReceiver struct {
	rw           *sync.RWMutex
	filename     string
	out          io.Writer
	formatter    string
	flags        *[]FlagPart
	isCallerInfo bool
	stats        *receiverStats
	isClosed     bool
	rotatePolicy string
	openDay      int
	isUTC        bool
	maxSize      int64
	maxLines     int64
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// FileReceiver methods
//___________________________________

// Init method initializes the file receiver instance.
func (f *FileReceiver) Init(cfg *config.Config) error {
	// File
	f.filename = cfg.StringDefault("log.file", "")
	if ess.IsStrEmpty(f.filename) {
		return errors.New("log: file value is required")
	}

	if err := f.openFile(); err != nil {
		return err
	}

	f.formatter = cfg.StringDefault("log.format", "text")
	if !(f.formatter == textFmt || f.formatter == jsonFmt) {
		return fmt.Errorf("log: unsupported format '%s'", f.formatter)
	}

	f.rotatePolicy = cfg.StringDefault("log.rotate.policy", "daily")
	switch f.rotatePolicy {
	case "daily":
		f.openDay = time.Now().Day()
	case "lines":
		f.maxLines = int64(cfg.IntDefault("log.rotate.lines", 0))
	case "size":
		maxSize, err := ess.StrToBytes(cfg.StringDefault("log.rotate.size", "512mb"))
		if err != nil {
			return err
		}
		f.maxSize = maxSize
	}

	return nil
}

// SetPattern method initializes the logger format pattern.
func (f *FileReceiver) SetPattern(pattern string) error {
	f.rw.Lock()
	defer f.rw.Unlock()
	flags, err := parseFlag(pattern)
	if err != nil {
		return err
	}
	f.flags = flags
	if f.formatter == textFmt {
		f.isCallerInfo = isCallerInfo(f.flags)
	}
	f.isUTC = isFmtFlagExists(f.flags, FmtFlagUTCTime)
	if f.isUTC {
		f.openDay = time.Now().UTC().Day()
	}
	return nil
}

// IsCallerInfo method returns true if log receiver is configured with caller info
// otherwise false.
func (f *FileReceiver) IsCallerInfo() bool {
	return f.isCallerInfo
}

// Log method logs the given entry values into file.
func (f *FileReceiver) Log(entry *Entry) {
	f.rw.RLock()
	defer f.rw.RUnlock()
	if f.isRotate() {
		_ = f.rotateFile()
	}

	msg := applyFormatter(f.formatter, f.flags, entry)
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		msg = append(msg, '\n')
	}

	size, _ := f.out.Write(msg)
	if size == 0 {
		return
	}

	// calculate receiver stats
	f.stats.bytes += int64(size)
	f.stats.lines++
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// FileReceiver Unexported methods
//___________________________________

func (f *FileReceiver) isRotate() bool {
	switch f.rotatePolicy {
	case "daily":
		if f.isUTC {
			return time.Now().UTC().Day() != f.openDay
		}
		return time.Now().Day() != f.openDay
	case "lines":
		return f.maxLines != 0 && f.stats.lines >= f.maxLines
	case "size":
		return f.maxSize != 0 && f.stats.bytes >= f.maxSize
	}
	return false
}

func (f *FileReceiver) rotateFile() error {
	if _, err := os.Lstat(f.filename); err == nil {
		f.close()
		if err = os.Rename(f.filename, f.backupFileName()); err != nil {
			return err
		}
	}

	if err := f.openFile(); err != nil {
		return err
	}

	return nil
}

func (f *FileReceiver) openFile() error {
	dir := filepath.Dir(f.filename)
	_ = ess.MkDirAll(dir, filePermission)

	file, err := os.OpenFile(f.filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, filePermission)
	if err != nil {
		return err
	}

	fileStat, err := file.Stat()
	if err != nil {
		return err
	}

	f.out = file
	f.isClosed = false
	f.stats = &receiverStats{}
	f.stats.bytes = fileStat.Size()
	f.stats.lines = int64(ess.LineCntr(file))

	return nil
}

func (f *FileReceiver) close() {
	if f.isClosed {
		return
	}
	ess.CloseQuietly(f.out)
	f.isClosed = true
}

func (f *FileReceiver) backupFileName() string {
	dir := filepath.Dir(f.filename)
	fileName := filepath.Base(f.filename)
	ext := filepath.Ext(fileName)
	baseName := ess.StripExt(fileName)
	t := time.Now()
	if f.isUTC {
		t = t.UTC()
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s%s", baseName, t.Format(BackupTimeFormat), ext))
}
