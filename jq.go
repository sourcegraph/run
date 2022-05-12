package run

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/itchyny/gojq"
)

// buildJQ parses and compiles a jq query.
func buildJQ(query string) (*gojq.Code, error) {
	jq, err := gojq.Parse(query)
	if err != nil {
		return nil, fmt.Errorf("jq.Parse: %w", err)
	}
	jqCode, err := gojq.Compile(jq)
	if err != nil {
		return nil, fmt.Errorf("jq.Compile: %w", err)
	}
	return jqCode, nil
}

// execJQ executes the compiled jq query against content.
func execJQ(jqCode *gojq.Code, content []byte) ([]byte, error) {
	if len(content) == 0 {
		return nil, nil
	}

	var input interface{}
	if err := json.NewDecoder(bytes.NewReader(content)).Decode(&input); err != nil {
		return nil, fmt.Errorf("json: %w: %s", err, string(content))
	}

	var newLine bytes.Buffer
	iter := jqCode.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}

		if err, ok := v.(error); ok {
			return nil, fmt.Errorf("jq: %w: %s", err, string(content))
		}

		result, err := gojq.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("jq: %w: %s", err, string(content))
		}
		newLine.Write(result)
	}
	return newLine.Bytes(), nil
}
