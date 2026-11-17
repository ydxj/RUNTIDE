package capsule

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/runtide/runtide/internal/domain"
	winapi "github.com/runtide/runtide/internal/platform/windows"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain doubles as the helper program the tests run as a real child.
func TestMain(m *testing.M) {
	switch os.Getenv("CAPSULE_HELPER") {
	case "":
		os.Exit(m.Run())
	case "talk":
		fmt.Println("hello out")
		fmt.Fprintln(os.Stderr, "hello err")
		fmt.Println("visible:", os.Getenv("VISIBLE"), "leaked:", os.Getenv("RUNTIDE_PARENT_SECRET"))
		os.Exit(7)
	case "sleep":
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		fmt.Println("ready")
		<-c
		fmt.Println("graceful")
		os.Exit(0)
	case "stubborn":
		signal.Ignore(os.Interrupt)
		fmt.Println("ready")
		time.Sleep(time.Hour)
	case "parent":
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "CAPSULE_HELPER=stubborn")
		_ = child.Start()
		fmt.Println("child", child.Process.Pid)
		time.Sleep(time.Hour)
	}
}

type sink struct {
	mu    sync.Mutex
	lines []string
	pid   int
	start int64
}

func (s *sink) Line(stream, line string) {
	s.mu.Lock()
	s.lines = append(s.lines, stream+":"+line)
	s.mu.Unlock()
}
func (s *sink) Started(pid int, ns int64) { s.mu.Lock(); s.pid, s.start = pid, ns; s.mu.Unlock() }
func (s *sink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.lines, "\n")
}

func helper(mode string) Spec {
	return Spec{Executable: os.Args[0], Env: map[string]string{"CAPSULE_HELPER": mode, "VISIBLE": "yes"}, Dir: os.TempDir()}
}

func TestRunCapturesStreamsExitCodeAndFiltersEnvironment(t *testing.T) {
	t.Setenv("RUNTIDE_PARENT_SECRET", "s3cret")
	s := &sink{}
	r := Run(context.Background(), helper("talk"), s)
	require.NoError(t, r.Err)
	assert.Equal(t, 7, r.ExitCode)
	assert.False(t, r.Cancelled)
	txt := s.text()
	assert.Contains(t, txt, "stdout:hello out")
	assert.Contains(t, txt, "stderr:hello err")
	assert.Contains(t, txt, "visible: yes")
	assert.Contains(t, txt, "leaked: \n", "parent secrets must not be inherited")
	assert.NotContains(t, txt, "s3cret")
	assert.NotZero(t, s.pid)
	assert.NotZero(t, s.start)
}

func TestInheritAllPassesParentEnvironment(t *testing.T) {
	t.Setenv("RUNTIDE_PARENT_SECRET", "s3cret")
	sp := helper("talk")
	sp.InheritAll = true
	s := &sink{}
	Run(context.Background(), sp, s)
	assert.Contains(t, s.text(), "leaked: s3cret")
}

func TestCancelIsGracefulWhenPossible(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &sink{}
	done := make(chan Result, 1)
	go func() { done <- Run(ctx, helper("sleep"), s) }()
	require.Eventually(t, func() bool { return strings.Contains(s.text(), "ready") }, 10*time.Second, 50*time.Millisecond)
	start := time.Now()
	cancel()
	r := <-done
	assert.True(t, r.Cancelled)
	assert.Equal(t, 0, r.ExitCode, "exit 0 proves the interrupt was delivered to the child")
	assert.Less(t, time.Since(start), 4*time.Second)
	assert.Contains(t, s.text(), "graceful")
}

func TestTimeoutKillsStubbornProcessTree(t *testing.T) {
	sp := helper("parent")
	sp.Timeout = 1500 * time.Millisecond
	s := &sink{}
	start := time.Now()
	r := Run(context.Background(), sp, s)
	assert.True(t, r.TimedOut)
	assert.Less(t, time.Since(start), 15*time.Second)
	var childPID uint32
	for _, l := range s.lines {
		fmt.Sscanf(l, "stdout:child %d", &childPID)
	}
	require.NotZero(t, childPID)
	assert.Eventually(t, func() bool { return !winapi.IsRunning(childPID) }, 5*time.Second, 50*time.Millisecond, "grandchild must die with the job")
}

func TestStartFailureIsReportedNotSwallowed(t *testing.T) {
	r := Run(context.Background(), Spec{Executable: "definitely-not-a-real-binary-xyz", Dir: os.TempDir()}, nil)
	require.Error(t, r.Err)
	assert.Contains(t, r.Err.Error(), "not found")
	assert.Equal(t, -1, r.ExitCode)
}

func TestBuildEnvOverridesCaseInsensitively(t *testing.T) {
	t.Setenv("PATH", "C:/x")
	env := BuildEnv(map[string]string{"Path": "D:/y", "NEW": "1"}, false)
	var paths []string
	for _, kv := range env {
		if strings.EqualFold(strings.SplitN(kv, "=", 2)[0], "path") {
			paths = append(paths, kv)
		}
	}
	assert.Equal(t, []string{"Path=D:/y"}, paths)
	assert.Contains(t, env, "NEW=1")
}

func TestDockerWrapUsesExplicitMountsAndNoSecretsOnCommandLine(t *testing.T) {
	ws := `D:\work\shop`
	spec := Spec{Executable: "npm", Args: []string{"test"}, Dir: ws, Env: map[string]string{"API_TOKEN": "topsecret", "PORT": "3000"}}
	w, cleanup, err := Wrap(domain.IsolationDocker, spec, "ex_1/unit", &DockerConfig{Image: "node:22-alpine", CPUs: 1.5, Memory: "2G", Network: "none", Ports: []string{"3000:3000"}}, nil)
	require.NoError(t, err)
	assert.Equal(t, "docker", w.Executable)
	joined := strings.Join(w.Args, " ")
	assert.Contains(t, joined, `-v D:\work\shop:/workspace`)
	assert.Contains(t, joined, "--cpus 1.5")
	assert.Contains(t, joined, "--memory 2g")
	assert.Contains(t, joined, "--network none")
	assert.Contains(t, joined, "-p 127.0.0.1:3000:3000")
	assert.Contains(t, joined, "-e API_TOKEN")
	assert.NotContains(t, joined, "topsecret", "values travel in the client environment, not on the command line")
	assert.True(t, strings.HasSuffix(joined, "node:22-alpine npm test"))
	assert.Equal(t, "topsecret", w.Env["API_TOKEN"])
	c := cleanup()
	assert.Equal(t, []string{"rm", "-f", "runtide-ex_1-unit"}, c.Args)
}

func TestDockerValidationRefusesDangerousMounts(t *testing.T) {
	ws := `D:\work\shop`
	for _, host := range []string{`C:\Users\me\.ssh`, `C:\Windows`, `C:\`, `\\.\pipe\docker_engine`, `C:\Users\me\AppData\Roaming`, `relative\path`} {
		err := ValidateDocker(DockerConfig{Image: "alpine", Mounts: []Mount{{Host: host, Target: "/x"}}}, ws)
		assert.Error(t, err, host)
	}
	assert.NoError(t, ValidateDocker(DockerConfig{Image: "alpine", Mounts: []Mount{{Host: `D:\work\shop\data`, Target: "/data"}}}, ws))
	assert.Error(t, ValidateDocker(DockerConfig{Image: "Bad Image!"}, ws))
	assert.Error(t, ValidateDocker(DockerConfig{Image: "alpine", Memory: "lots"}, ws))
	assert.Error(t, ValidateDocker(DockerConfig{Image: "alpine", Network: "host"}, ws))
	_, _, err := Wrap(domain.IsolationDocker, Spec{Dir: ws}, "x", nil, nil)
	assert.Error(t, err)
}

func TestWSLWrap(t *testing.T) {
	w, _, err := Wrap(domain.IsolationWSL, Spec{Executable: "ls", Args: []string{"-la"}, Dir: `D:\work`, Env: map[string]string{"B": "2", "A": "1"}}, "x", nil, &WSLConfig{Distro: "Ubuntu-22.04"})
	require.NoError(t, err)
	assert.Equal(t, "wsl.exe", w.Executable)
	assert.Equal(t, []string{"-d", "Ubuntu-22.04", "--cd", `D:\work`, "--exec", "ls", "-la"}, w.Args)
	assert.Equal(t, "A:B", w.Env["WSLENV"])
	_, _, err = Wrap(domain.IsolationWSL, Spec{}, "x", nil, &WSLConfig{Distro: "bad distro;rm"})
	assert.Error(t, err)
}
