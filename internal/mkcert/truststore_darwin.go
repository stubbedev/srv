// Copyright 2018 The mkcert Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mkcert

import (
	"bytes"
	"encoding/asn1"
	"fmt"
	"os"

	"howett.net/plist"
)

var (
	firefoxProfiles     = []string{os.Getenv("HOME") + "/Library/Application Support/Firefox/Profiles/*"}
	certutilInstallHelp = "brew install nss"
	nssBrowsers         = "Firefox"
)

// https://github.com/golang/go/issues/24652#issuecomment-399826583
var trustSettings []any

var _, _ = plist.Unmarshal(trustSettingsData, &trustSettings)

var trustSettingsData = []byte(`
<array>
	<dict>
		<key>kSecTrustSettingsPolicy</key>
		<data>
		KoZIhvdjZAED
		</data>
		<key>kSecTrustSettingsPolicyName</key>
		<string>sslServer</string>
		<key>kSecTrustSettingsResult</key>
		<integer>1</integer>
	</dict>
	<dict>
		<key>kSecTrustSettingsPolicy</key>
		<data>
		KoZIhvdjZAEC
		</data>
		<key>kSecTrustSettingsPolicyName</key>
		<string>basicX509</string>
		<key>kSecTrustSettingsResult</key>
		<integer>1</integer>
	</dict>
</array>
`)

func (ca *localCA) installPlatform(_ *InstallResult) (bool, error) {
	if err := privileged(nil, "security", "add-trusted-cert", "-d", "-k", "/Library/Keychains/System.keychain", ca.path(rootName)); err != nil {
		return false, err
	}

	// Make trustSettings explicit, as older Go does not know the defaults.
	// https://github.com/golang/go/issues/24652

	plistFile, err := os.CreateTemp("", "trust-settings")
	if err != nil {
		return false, fmt.Errorf("failed to create temp file: %w", err)
	}
	plistName := plistFile.Name()
	defer os.Remove(plistName)
	plistFile.Close()

	if err := privileged(nil, "security", "trust-settings-export", "-d", plistName); err != nil {
		return false, err
	}

	plistData, err := os.ReadFile(plistName)
	if err != nil {
		return false, fmt.Errorf("failed to read trust settings: %w", err)
	}
	var plistRoot map[string]any
	if _, err := plist.Unmarshal(plistData, &plistRoot); err != nil {
		return false, fmt.Errorf("failed to parse trust settings: %w", err)
	}

	rootSubjectASN1, _ := asn1.Marshal(ca.caCert.Subject.ToRDNSequence())

	if plistRoot["trustVersion"].(uint64) != 1 {
		return false, fmt.Errorf("unsupported trust settings version: %v", plistRoot["trustVersion"])
	}
	trustList := plistRoot["trustList"].(map[string]interface{})
	for key := range trustList {
		entry := trustList[key].(map[string]interface{})
		if _, ok := entry["issuerName"]; !ok {
			continue
		}
		issuerName := entry["issuerName"].([]byte)
		if !bytes.Equal(rootSubjectASN1, issuerName) {
			continue
		}
		entry["trustSettings"] = trustSettings
		break
	}

	plistData, err = plist.MarshalIndent(plistRoot, plist.XMLFormat, "\t")
	if err != nil {
		return false, fmt.Errorf("failed to serialize trust settings: %w", err)
	}
	if err := os.WriteFile(plistName, plistData, 0o600); err != nil {
		return false, fmt.Errorf("failed to write trust settings: %w", err)
	}

	if err := privileged(nil, "security", "trust-settings-import", "-d", plistName); err != nil {
		return false, err
	}

	return true, nil
}

func (ca *localCA) uninstallPlatform() (bool, error) {
	if err := privileged(nil, "security", "remove-trusted-cert", "-d", ca.path(rootName)); err != nil {
		return false, err
	}
	return true, nil
}
