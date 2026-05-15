package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

func runHash(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return err
	}

	sum := h.Sum(nil)
	fmt.Printf("%s  %s\n", hex.EncodeToString(sum), path)
	return nil
}
