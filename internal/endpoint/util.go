package endpoint

import (
	"os"
	"strconv"
	"time"
)

const defaultDialTimeout = 500 * time.Millisecond

func intString(value int) string {
	return strconv.Itoa(value)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
