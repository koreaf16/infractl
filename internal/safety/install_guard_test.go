// Package safety
// File: install_guard_test.go
// Description: InstallContextReason 함수의 설치 패턴 매칭 테스트
// Responsibility: 기존 및 신규 설치 패턴이 올바르게 감지되는지 검증

package safety

import "testing"

func TestInstallContextReasonMatchesPackageManagers(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"apt-get install -y nginx", "package installation"},
		{"yum install httpd", "package installation"},
		{"dnf install -y postgresql-server", "package installation"},
		{"pacman -S docker", "package installation"},
		{"brew install go", "package installation"},
		{"rpm --install package.rpm", "package installation"},
	}
	for _, tc := range cases {
		got := InstallContextReason(tc.command)
		if got != tc.want {
			t.Errorf("InstallContextReason(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestInstallContextReasonMatchesDevTools(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"pip install flask", "tool or runtime installation"},
		{"pip3 install django", "tool or runtime installation"},
		{"npm install express", "tool or runtime installation"},
		{"gem install rails", "tool or runtime installation"},
		{"cargo install ripgrep", "tool or runtime installation"},
		{"go install golang.org/x/tools/gopls@latest", "tool or runtime installation"},
	}
	for _, tc := range cases {
		got := InstallContextReason(tc.command)
		if got != tc.want {
			t.Errorf("InstallContextReason(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestInstallContextReasonMatchesBuildInstall(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"make install", "build install step"},
		{"cmake --install build", "build install step"},
	}
	for _, tc := range cases {
		got := InstallContextReason(tc.command)
		if got != tc.want {
			t.Errorf("InstallContextReason(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestInstallContextReasonMatchesContainerOps(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"docker pull mysql:8.0", "container image/stack setup"},
		{"docker compose up -d", "container image/stack setup"},
		{"helm install my-release bitnami/nginx", "kubernetes chart installation"},
		{"helm upgrade --install my-app ./chart", "kubernetes chart installation"},
	}
	for _, tc := range cases {
		got := InstallContextReason(tc.command)
		if got != tc.want {
			t.Errorf("InstallContextReason(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestInstallContextReasonMatchesUniversalPackages(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"snap install kubectl --classic", "universal package installation"},
		{"flatpak install flathub org.gimp.GIMP", "universal package installation"},
	}
	for _, tc := range cases {
		got := InstallContextReason(tc.command)
		if got != tc.want {
			t.Errorf("InstallContextReason(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestInstallContextReasonMatchesWindowsPackages(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"choco install nodejs -y", "windows package installation"},
		{"winget install Microsoft.VisualStudioCode", "windows package installation"},
		{"msiexec /i installer.msi", "windows package installation"},
	}
	for _, tc := range cases {
		got := InstallContextReason(tc.command)
		if got != tc.want {
			t.Errorf("InstallContextReason(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestInstallContextReasonMatchesScriptInstall(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"./setup.sh", "script-based installation"},
		{"./install.sh --prefix=/opt/app", "script-based installation"},
		{"./configure --prefix=/usr/local", "script-based installation"},
	}
	for _, tc := range cases {
		got := InstallContextReason(tc.command)
		if got != tc.want {
			t.Errorf("InstallContextReason(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestInstallContextReasonMatchesEnvSetup(t *testing.T) {
	cases := []struct {
		command string
		want    string
	}{
		{"export ORACLE_HOME=/u01/app/oracle/product/19c", "software environment setup"},
		{"export JAVA_HOME=/usr/lib/jvm/java-17", "software environment setup"},
		{"export CATALINA_HOME=/opt/tomcat", "software environment setup"},
	}
	for _, tc := range cases {
		got := InstallContextReason(tc.command)
		if got != tc.want {
			t.Errorf("InstallContextReason(%q) = %q, want %q", tc.command, got, tc.want)
		}
	}
}

func TestInstallContextReasonReturnsEmptyForNonInstall(t *testing.T) {
	nonInstallCommands := []string{
		"ls -la /opt",
		"cat /etc/os-release",
		"df -h",
		"systemctl status nginx",
		"ps aux | grep oracle",
		"echo hello",
	}
	for _, cmd := range nonInstallCommands {
		got := InstallContextReason(cmd)
		if got != "" {
			t.Errorf("InstallContextReason(%q) = %q, want empty", cmd, got)
		}
	}
}
