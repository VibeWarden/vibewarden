package tls

import (
	"encoding/pem"
	"os"
)

// readFileBytes reads all bytes from a file path.
func readFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path) //nolint:wrapcheck,gosec // caller wraps; path is operator-controlled config, not user input
}

// pemDecode strips PEM armor and returns the raw DER bytes.
// If the input is not PEM-encoded it is returned as-is (assumed to be DER).
func pemDecode(data []byte) []byte {
	var der []byte
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			der = append(der, block.Bytes...)
		}
	}
	if len(der) == 0 {
		// Input may already be DER.
		return data
	}
	return der
}
