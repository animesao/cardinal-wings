package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// consolelog.go persists typed console input into the container's log file.
//
// Why: the cardinal log only carries container OUTPUT. Session replay
// (attach tail) therefore restores answers without the commands that
// produced them — after every reconnect the view reads "logs, answer"
// with the command gone. Appending `> cmd` lines at send time puts the
// command back next to its answer in every future replay, for every
// client, with no browser storage involved.
//
// The file layout mirrors cardinal's state.LogPath: <data>/logs/<id>.log.
// Writes are O_APPEND from both console-serve and wings; individual lines
// are small, so interleaving stays line-granular.

// appendConsoleEcho records one typed input into the container log.
// Never fails the input path: errors are swallowed by design.
func appendConsoleEcho(containerID, input string) {
	id := resolveLocalID(containerID)
	if !validContainerID(id) {
		return
	}
	path := filepath.Join(cardinalDataDir(), "logs", safeLogPart(id)+".log")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, line := range strings.Split(input, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) > 4096 {
			line = line[:4096]
		}
		fmt.Fprintln(f, "> "+line)
	}
}

// safeLogPart mirrors cardinal's state.safePart so wings lands on the exact
// same log filename cardinal itself uses.
func safeLogPart(value string) string {
	if value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\\`) && filepath.Base(value) == value {
		return value
	}
	h := sha256.New()
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
