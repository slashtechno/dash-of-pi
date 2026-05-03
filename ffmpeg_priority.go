package main

import "os/exec"

func lowPriorityCommand(name string, args ...string) *exec.Cmd {
	cmdArgs := append([]string{}, args...)
	if _, err := exec.LookPath("nice"); err == nil {
		cmdArgs = append([]string{"-n", "19", name}, cmdArgs...)
		name = "nice"
	}
	if _, err := exec.LookPath("ionice"); err == nil {
		cmdArgs = append([]string{"-c", "3", name}, cmdArgs...)
		name = "ionice"
	}
	return exec.Command(name, cmdArgs...)
}
