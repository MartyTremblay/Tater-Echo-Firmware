package microwakeword

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureEmbeddedDefaultPackageInstallsPairedModelsAndRepairsDamage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TATER_MWW_DIR", dir)

	updated, err := EnsureEmbeddedDefaultPackage()
	if err != nil || !updated {
		t.Fatalf("first install updated=%v err=%v", updated, err)
	}
	for _, name := range []string{
		defaultManifestFilename,
		defaultModelFilename,
		defaultOWWMetadataFilename,
		defaultOWWONNXFilename,
		defaultNanoCNNFilename,
		defaultNanoGRUFilename,
		defaultBundleFilename,
	} {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.Size() == 0 {
			t.Fatalf("installed %s info=%v err=%v", name, info, err)
		}
	}
	if paired, err := PairedCompanionPackage(DefaultPackage); err != nil || paired != DefaultPackage {
		t.Fatalf("paired=%q err=%v", paired, err)
	}
	bundleBody, err := os.ReadFile(filepath.Join(dir, defaultBundleFilename))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := ParseWakeBundle(bundleBody)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.OpenWakeWord.RecommendedThreshold != 0.74 ||
		bundle.OpenWakeWord.RecommendedPatience != 3 ||
		bundle.OpenWakeWord.RecommendedConfirmationThreshold != 0.96 ||
		bundle.OpenWakeWord.RecommendedConfirmationPatience != 3 {
		t.Fatalf("embedded OWW policy = %.2f/%d confirmation %.2f/%d, want 0.74/3 and 0.96/3",
			bundle.OpenWakeWord.RecommendedThreshold,
			bundle.OpenWakeWord.RecommendedPatience,
			bundle.OpenWakeWord.RecommendedConfirmationThreshold,
			bundle.OpenWakeWord.RecommendedConfirmationPatience)
	}
	for _, architecture := range []string{"cnn", "gru"} {
		variant, _, err := nanoVariant(bundle, architecture)
		if err != nil || variant.RecommendedThreshold != 0.99 || variant.RecommendedPatience != 3 ||
			!cachedNanoValid(DefaultPackage, architecture) {
			t.Fatalf("embedded NWW %s variant=%+v cached=%v err=%v",
				architecture, variant, cachedNanoValid(DefaultPackage, architecture), err)
		}
	}
	updated, err = EnsureEmbeddedDefaultPackage()
	if err != nil || updated {
		t.Fatalf("cached install updated=%v err=%v", updated, err)
	}
	if err := os.WriteFile(filepath.Join(dir, defaultOWWONNXFilename), []byte("damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	updated, err = EnsureEmbeddedDefaultPackage()
	if err != nil || !updated || !CompanionReady(DefaultPackage) {
		t.Fatalf("repair updated=%v ready=%v err=%v", updated, CompanionReady(DefaultPackage), err)
	}
	if err := os.WriteFile(filepath.Join(dir, defaultNanoGRUFilename), []byte("damaged"), 0o644); err != nil {
		t.Fatal(err)
	}
	updated, err = EnsureEmbeddedDefaultPackage()
	if err != nil || !updated || !cachedNanoValid(DefaultPackage, "gru") {
		t.Fatalf("NWW repair updated=%v ready=%v err=%v", updated, cachedNanoValid(DefaultPackage, "gru"), err)
	}
}
