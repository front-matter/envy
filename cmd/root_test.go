package cmd

import "testing"

func TestRootPersistentFileFlag(t *testing.T) {
	fileFlag := rootCmd.PersistentFlags().Lookup("file")
	if fileFlag == nil {
		t.Fatal("expected persistent --file flag to be registered")
	}
	if fileFlag.Shorthand != "f" {
		t.Fatalf("expected --file shorthand to be -f, got %q", fileFlag.Shorthand)
	}

	if manifestFlag := rootCmd.PersistentFlags().Lookup("manifest"); manifestFlag != nil {
		t.Fatal("expected deprecated --manifest flag to be absent")
	}
}
