package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

const execTimeout = 30 * time.Second

func runExecCommand(ctx context.Context, command string, stdin []byte) error {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(stdin)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exec %q: %w (output: %s)", command, err, string(output))
	}
	return nil
}

func runExecCommandOutput(ctx context.Context, command string, stdin []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("exec %q: %w (stderr: %s)", command, err, stderr.String())
	}
	return stdout.Bytes(), nil
}
