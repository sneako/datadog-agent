package config

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/DataDog/gopsutil/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var originalConfig = config.Datadog

func restoreGlobalConfig() {
	config.Datadog = originalConfig
}

func TestBlacklist(t *testing.T) {
	testBlacklist := []string{
		"^getty",
		"^acpid",
		"^atd",
		"^upstart-udev-bridge",
		"^upstart-socket-bridge",
		"^upstart-file-bridge",
		"^dhclient",
		"^dhclient3",
		"^rpc",
		"^dbus-daemon",
		"udevd",
		"^/sbin/",
		"^/usr/sbin/",
		"^/var/ossec/bin/ossec",
		"^rsyslogd",
		"^whoopsie$",
		"^cron$",
		"^CRON$",
		"^/usr/lib/postfix/master$",
		"^qmgr",
		"^pickup",
		"^sleep",
		"^/lib/systemd/systemd-logind$",
		"^/usr/local/bin/goshe dnsmasq$",
	}
	blacklist := make([]*regexp.Regexp, 0, len(testBlacklist))
	for _, b := range testBlacklist {
		r, err := regexp.Compile(b)
		if err == nil {
			blacklist = append(blacklist, r)
		}
	}
	cases := []struct {
		cmdline     []string
		blacklisted bool
	}{
		{[]string{"getty", "-foo", "-bar"}, true},
		{[]string{"rpcbind", "-x"}, true},
		{[]string{"my-rpc-app", "-config foo.ini"}, false},
		{[]string{"rpc.statd", "-L"}, true},
		{[]string{"/usr/sbin/irqbalance"}, true},
	}

	for _, c := range cases {
		assert.Equal(t, c.blacklisted, IsBlacklisted(c.cmdline, blacklist),
			fmt.Sprintf("Case %v failed", c))
	}
}

func TestOnlyEnvConfig(t *testing.T) {
	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	defer restoreGlobalConfig()

	// setting an API Key should be enough to generate valid config
	os.Setenv("DD_API_KEY", "apikey_from_env")
	defer os.Unsetenv("DD_API_KEY")

	agentConfig, _ := NewAgentConfig("test", "", "")
	assert.Equal(t, "apikey_from_env", agentConfig.APIEndpoints[0].APIKey)
}

func TestOnlyEnvConfigArgsScrubbingEnabled(t *testing.T) {
	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	defer restoreGlobalConfig()

	os.Setenv("DD_CUSTOM_SENSITIVE_WORDS", "*password*,consul_token,*api_key")
	defer os.Unsetenv("DD_CUSTOM_SENSITIVE_WORDS")

	agentConfig, _ := NewAgentConfig("test", "", "")
	assert.Equal(t, true, agentConfig.Scrubber.Enabled)

	cases := []struct {
		cmdline       []string
		parsedCmdline []string
	}{
		{
			[]string{"spidly", "--mypasswords=123,456", "consul_token", "1234", "--dd_api_key=1234"},
			[]string{"spidly", "--mypasswords=********", "consul_token", "********", "--dd_api_key=********"},
		},
	}

	for i := range cases {
		cases[i].cmdline, _ = agentConfig.Scrubber.scrubCommand(cases[i].cmdline)
		assert.Equal(t, cases[i].parsedCmdline, cases[i].cmdline)
	}
}

func TestOnlyEnvConfigArgsScrubbingDisabled(t *testing.T) {
	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	defer restoreGlobalConfig()

	os.Setenv("DD_SCRUB_ARGS", "false")
	os.Setenv("DD_CUSTOM_SENSITIVE_WORDS", "*password*,consul_token,*api_key")
	defer os.Unsetenv("DD_SCRUB_ARGS")
	defer os.Unsetenv("DD_CUSTOM_SENSITIVE_WORDS")

	agentConfig, _ := NewAgentConfig("test", "", "")
	assert.Equal(t, false, agentConfig.Scrubber.Enabled)

	cases := []struct {
		cmdline       []string
		parsedCmdline []string
	}{
		{
			[]string{"spidly", "--mypasswords=123,456", "consul_token", "1234", "--dd_api_key=1234"},
			[]string{"spidly", "--mypasswords=123,456", "consul_token", "1234", "--dd_api_key=1234"},
		},
	}

	for i := range cases {
		fp := &process.FilledProcess{Cmdline: cases[i].cmdline}
		cases[i].cmdline = agentConfig.Scrubber.ScrubProcessCommand(fp)
		assert.Equal(t, cases[i].parsedCmdline, cases[i].cmdline)
	}
}

func TestGetHostname(t *testing.T) {
	cfg := NewDefaultAgentConfig()
	h, err := getHostname(cfg.DDAgentBin)
	assert.Nil(t, err)
	assert.NotEqual(t, "", h)
}

func TestDefaultConfig(t *testing.T) {
	assert := assert.New(t)
	agentConfig := NewDefaultAgentConfig()

	// assert that some sane defaults are set
	assert.Equal("info", agentConfig.LogLevel)
	assert.Equal(true, agentConfig.AllowRealTime)
	assert.Equal(containerChecks, agentConfig.EnabledChecks)
	assert.Equal(true, agentConfig.Scrubber.Enabled)

	os.Setenv("DOCKER_DD_AGENT", "yes")
	agentConfig = NewDefaultAgentConfig()
	assert.Equal(os.Getenv("HOST_PROC"), "")
	assert.Equal(os.Getenv("HOST_SYS"), "")
	os.Setenv("DOCKER_DD_AGENT", "no")
	assert.Equal(containerChecks, agentConfig.EnabledChecks)
	assert.Equal(6062, agentConfig.ProcessExpVarPort)

	os.Unsetenv("DOCKER_DD_AGENT")
}

var secretScriptBuilder sync.Once

func setupSecretScript() error {
	script := "./testdata/secret"
	goCmd, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("Couldn't find golang binary in path")
	}

	buildCmd := exec.Command(goCmd, "build", "-o", script, fmt.Sprintf("%s.go", script))
	if err := buildCmd.Start(); err != nil {
		return fmt.Errorf("Couldn't build script %v: %s", script, err)
	}
	if err := buildCmd.Wait(); err != nil {
		return fmt.Errorf("Couldn't wait the end of the build for script %v: %s", script, err)
	}

	// Permissions required for the secret script
	err = os.Chmod(script, 0700)
	if err != nil {
		return err
	}

	return os.Chown(script, os.Geteuid(), os.Getgid())
}

func TestAgentConfigYamlEnc(t *testing.T) {
	secretScriptBuilder.Do(func() { require.NoError(t, setupSecretScript()) })

	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	defer restoreGlobalConfig()
	// Secrets settings are initialized only once by initConfig in the agent package so we have to setup them
	config.Datadog.Set("secret_backend_timeout", 15)
	config.Datadog.Set("secret_backend_output_max_size", 1024)

	assert := assert.New(t)

	agentConfig, err := NewAgentConfig(
		"test",
		"./testdata/TestDDAgentConfigYamlEnc.yaml",
		"",
	)
	assert.NoError(err)

	ep := agentConfig.APIEndpoints[0]
	assert.Equal("secret_my_api_key", ep.APIKey)
}

func TestAgentConfigYamlEnc2(t *testing.T) {
	secretScriptBuilder.Do(func() { require.NoError(t, setupSecretScript()) })

	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	defer restoreGlobalConfig()
	// Secrets settings are initialized only once by initConfig in the agent package so we have to setup them
	config.Datadog.Set("secret_backend_timeout", 15)
	config.Datadog.Set("secret_backend_output_max_size", 1024)
	assert := assert.New(t)
	agentConfig, err := NewAgentConfig(
		"test",
		"./testdata/TestDDAgentConfigYamlEnc2.yaml",
		"",
	)
	assert.NoError(err)

	ep := agentConfig.APIEndpoints[0]
	assert.Equal("secret_encrypted_key", ep.APIKey)
	assert.Equal("secret_burrito.com", ep.Endpoint.String())
}

func TestAgentConfigYamlAndNetworkConfig(t *testing.T) {
	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	defer restoreGlobalConfig()

	assert := assert.New(t)

	agentConfig, err := NewAgentConfig(
		"test",
		"./testdata/TestDDAgentConfigYamlAndNetworkConfig.yaml",
		"",
	)
	assert.NoError(err)

	ep := agentConfig.APIEndpoints[0]
	assert.Equal("apikey_20", ep.APIKey)
	assert.Equal("my-process-app.datadoghq.com", ep.Endpoint.Hostname())
	assert.Equal(10, agentConfig.QueueSize)
	assert.Equal(true, agentConfig.AllowRealTime)
	assert.Equal(true, agentConfig.Enabled)
	assert.Equal(processChecks, agentConfig.EnabledChecks)
	assert.Equal(8*time.Second, agentConfig.CheckIntervals["container"])
	assert.Equal(30*time.Second, agentConfig.CheckIntervals["process"])
	assert.Equal(100, agentConfig.Windows.ArgsRefreshInterval)
	assert.Equal(false, agentConfig.Windows.AddNewArgs)
	assert.Equal(false, agentConfig.Scrubber.Enabled)
	assert.Equal(5065, agentConfig.ProcessExpVarPort)

	agentConfig, err = NewAgentConfig(
		"test",
		"./testdata/TestDDAgentConfigYamlAndNetworkConfig.yaml",
		"./testdata/TestDDAgentConfigYamlAndNetworkConfig-Net.yaml",
	)

	assert.Equal("apikey_20", ep.APIKey)
	assert.Equal("my-process-app.datadoghq.com", ep.Endpoint.Hostname())
	assert.Equal(10, agentConfig.QueueSize)
	assert.Equal(true, agentConfig.AllowRealTime)
	assert.Equal(true, agentConfig.Enabled)
	assert.Equal(8*time.Second, agentConfig.CheckIntervals["container"])
	assert.Equal(30*time.Second, agentConfig.CheckIntervals["process"])
	assert.Equal(100, agentConfig.Windows.ArgsRefreshInterval)
	assert.Equal(false, agentConfig.Windows.AddNewArgs)
	assert.Equal(false, agentConfig.Scrubber.Enabled)
	assert.Equal("/var/my-location/network-tracer.log", agentConfig.NetworkTracerSocketPath)
	assert.Equal(append(processChecks, "connections"), agentConfig.EnabledChecks)
	assert.True(agentConfig.NetworkBPFDebug)
	assert.Empty(agentConfig.ExcludedBPFLinuxVersions)
	assert.False(agentConfig.DisableTCPTracing)
	assert.False(agentConfig.DisableUDPTracing)
	assert.False(agentConfig.DisableIPv6Tracing)

	agentConfig, err = NewAgentConfig(
		"test",
		"./testdata/TestDDAgentConfigYamlAndNetworkConfig.yaml",
		"./testdata/TestDDAgentConfigYamlAndNetworkConfig-Net-2.yaml",
	)

	assert.Equal("apikey_20", ep.APIKey)
	assert.Equal("my-process-app.datadoghq.com", ep.Endpoint.Hostname())
	assert.Equal(10, agentConfig.QueueSize)
	assert.Equal(true, agentConfig.AllowRealTime)
	assert.Equal(true, agentConfig.Enabled)
	assert.Equal(8*time.Second, agentConfig.CheckIntervals["container"])
	assert.Equal(30*time.Second, agentConfig.CheckIntervals["process"])
	assert.Equal(100, agentConfig.Windows.ArgsRefreshInterval)
	assert.Equal(false, agentConfig.Windows.AddNewArgs)
	assert.Equal(false, agentConfig.Scrubber.Enabled)
	assert.False(agentConfig.NetworkBPFDebug)
	assert.Equal(agentConfig.ExcludedBPFLinuxVersions, []string{"5.5.0", "4.2.1"})
	assert.Equal("/var/my-location/network-tracer.log", agentConfig.NetworkTracerSocketPath)
	assert.Equal(append(processChecks, "connections"), agentConfig.EnabledChecks)
	assert.True(agentConfig.DisableTCPTracing)
	assert.True(agentConfig.DisableUDPTracing)
	assert.True(agentConfig.DisableIPv6Tracing)
}

func TestProxyEnv(t *testing.T) {
	assert := assert.New(t)
	for i, tc := range []struct {
		host     string
		port     int
		user     string
		pass     string
		expected string
	}{
		{
			"example.com",
			1234,
			"",
			"",
			"http://example.com:1234",
		},
		{
			"https://example.com",
			4567,
			"foo",
			"bar",
			"https://foo:bar@example.com:4567",
		},
		{
			"example.com",
			0,
			"foo",
			"",
			"http://foo@example.com:3128",
		},
	} {
		os.Setenv("PROXY_HOST", tc.host)
		if tc.port > 0 {
			os.Setenv("PROXY_PORT", strconv.Itoa(tc.port))
		} else {
			os.Setenv("PROXY_PORT", "")
		}
		os.Setenv("PROXY_USER", tc.user)
		os.Setenv("PROXY_PASSWORD", tc.pass)
		pf, err := proxyFromEnv(nil)
		assert.NoError(err, "proxy case %d had error", i)
		u, err := pf(&http.Request{})
		assert.NoError(err)
		assert.Equal(tc.expected, u.String())
	}

	os.Unsetenv("PROXY_HOST")
	os.Unsetenv("PROXY_PORT")
	os.Unsetenv("PROXY_USER")
	os.Unsetenv("PROXY_PASSWORD")
}

func TestEnvSiteConfig(t *testing.T) {
	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	defer restoreGlobalConfig()

	assert := assert.New(t)

	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	agentConfig, err := NewAgentConfig("test", "./testdata/TestEnvSiteConfig.yaml", "")
	assert.NoError(err)
	assert.Equal("process.datadoghq.io", agentConfig.APIEndpoints[0].Endpoint.Hostname())

	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	agentConfig, err = NewAgentConfig("test", "./testdata/TestEnvSiteConfig-2.yaml", "")
	assert.NoError(err)
	assert.Equal("process.datadoghq.eu", agentConfig.APIEndpoints[0].Endpoint.Hostname())

	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	agentConfig, err = NewAgentConfig("test", "./testdata/TestEnvSiteConfig-3.yaml", "")
	assert.NoError(err)
	assert.Equal("burrito.com", agentConfig.APIEndpoints[0].Endpoint.Hostname())

	config.Datadog = config.NewConfig("datadog", "DD", strings.NewReplacer(".", "_"))
	os.Setenv("DD_PROCESS_AGENT_URL", "https://test.com")
	agentConfig, err = NewAgentConfig("test", "./testdata/TestEnvSiteConfig-3.yaml", "")
	assert.Equal("test.com", agentConfig.APIEndpoints[0].Endpoint.Hostname())

	os.Unsetenv("DD_PROCESS_AGENT_URL")
}

func TestIsAffirmative(t *testing.T) {
	value, err := isAffirmative("yes")
	assert.Nil(t, err)
	assert.True(t, value)

	value, err = isAffirmative("True")
	assert.Nil(t, err)
	assert.True(t, value)

	value, err = isAffirmative("1")
	assert.Nil(t, err)
	assert.True(t, value)

	_, err = isAffirmative("")
	assert.NotNil(t, err)

	value, err = isAffirmative("ok")
	assert.Nil(t, err)
	assert.False(t, value)
}