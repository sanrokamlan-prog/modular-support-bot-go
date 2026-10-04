package main

import (
	"regexp"
	"strconv"
	"testing"
)

func TestNewChallengeIsMultiStepAndCorrect(t *testing.T) {
	for i := 0; i < 25; i++ {
		challenge, err := newChallenge()
		if err != nil {
			t.Fatal(err)
		}
		match := regexp.MustCompile(`\((\d+) \+ (\d+)\) × (\d+) - (\d+) = \?`).FindStringSubmatch(challenge.Question)
		if len(match) != 5 {
			t.Fatalf("unexpected challenge format: %q", challenge.Question)
		}
		a, _ := strconv.Atoi(match[1])
		b, _ := strconv.Atoi(match[2])
		c, _ := strconv.Atoi(match[3])
		d, _ := strconv.Atoi(match[4])
		if challenge.Answer != (a+b)*c-d {
			t.Fatalf("wrong answer for %q: %d", challenge.Question, challenge.Answer)
		}
	}
}
