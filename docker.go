package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"github.com/blang/semver"
	"github.com/coveo/gotemplate/utils"
	"github.com/fatih/color"
	"github.com/gruntwork-io/terragrunt/util"
)

func callDocker(args ...string) int {
	command := append([]string{config.EntryPoint}, args...)

	// Change the default log level for terragrunt
	const logLevelArg = "--terragrunt-logging-level"
	if !util.ListContainsElement(command, logLevelArg) && filepath.Base(config.EntryPoint) == "terragrunt" {
		if config.LogLevel == "6" || strings.ToLower(config.LogLevel) == "full" {
			config.LogLevel = "debug"
			config.Environment["TF_LOG"] = "DEBUG"
			config.Environment["TERRAGRUNT_DEBUG"] = "1"
		}
	}

	if flushCache && filepath.Base(config.EntryPoint) == "terragrunt" {
		command = append(command, "--terragrunt-source-update")
	}

	imageName := getImage()

	if getImageName {
		fmt.Println(imageName)
		return 0
	}

	cwd := filepath.ToSlash(Must(filepath.EvalSymlinks(Must(os.Getwd()).(string))).(string))
	currentDrive := fmt.Sprintf("%s/", filepath.VolumeName(cwd))
	sourceFolder := filepath.ToSlash(filepath.Join("/", mountPoint, strings.TrimPrefix(cwd, currentDrive)))
	rootFolder := strings.Split(strings.TrimPrefix(cwd, currentDrive), "/")[0]

	dockerArgs := []string{
		"run", "-it",
		"-v", fmt.Sprintf("%s%s:%s", convertDrive(currentDrive), rootFolder, filepath.ToSlash(filepath.Join("/", mountPoint, rootFolder))),
		"-w", sourceFolder,
	}
	if !noHome {
		currentUser := Must(user.Current()).(*user.User)
		home := filepath.ToSlash(currentUser.HomeDir)
		homeWithoutVolume := strings.TrimPrefix(home, filepath.VolumeName(home))

		dockerArgs = append(dockerArgs, []string{
			"-v", fmt.Sprintf("%v:%v", convertDrive(home), homeWithoutVolume),
			"-e", fmt.Sprintf("HOME=%v", homeWithoutVolume),
		}...)

		dockerArgs = append(dockerArgs, config.DockerOptions...)
	}

	if !noTemp {
		temp := filepath.ToSlash(filepath.Join(Must(filepath.EvalSymlinks(os.TempDir())).(string), "tgf-cache"))
		tempDrive := fmt.Sprintf("%s/", filepath.VolumeName(temp))
		tempFolder := strings.TrimPrefix(temp, tempDrive)
		dockerArgs = append(dockerArgs, "-v", fmt.Sprintf("%s%s:/var/tgf", convertDrive(tempDrive), tempFolder))
		config.Environment["TERRAGRUNT_CACHE"] = "/var/tgf"
	}

	config.Environment["TGF_COMMAND"] = config.EntryPoint
	config.Environment["TGF_VERSION"] = version
	config.Environment["TGF_ARGS"] = strings.Join(os.Args, " ")
	config.Environment["TGF_LAUNCH_FOLDER"] = sourceFolder
	config.Environment["TGF_IMAGE_NAME"] = imageName // sha256 of image

	if !strings.Contains(config.Image, "coveo/tgf") { // the tgf image injects its own image info
		config.Environment["TGF_IMAGE"] = config.Image
		if config.ImageVersion != nil {
			config.Environment["TGF_IMAGE_VERSION"] = *config.ImageVersion
			if version, err := semver.Make(*config.ImageVersion); err == nil {
				config.Environment["TGF_IMAGE_MAJ_MIN"] = fmt.Sprintf("%d.%d", version.Major, version.Minor)
			}
		}
		if config.ImageTag != nil {
			config.Environment["TGF_IMAGE_TAG"] = *config.ImageTag
		}
	}

	for key, val := range config.Environment {
		os.Setenv(key, val)
		if debug {
			printfDebug(os.Stderr, "export %v=%v\n", key, val)
		}
	}

	for _, do := range dockerOptions {
		dockerArgs = append(dockerArgs, strings.Split(do, " ")...)
	}

	if !util.ListContainsElement(dockerArgs, "--name") {
		// We do not remove the image after execution if a name has been provided
		dockerArgs = append(dockerArgs, "--rm")
	}

	dockerArgs = append(dockerArgs, getEnviron(!noHome)...)
	dockerArgs = append(dockerArgs, imageName)
	dockerArgs = append(dockerArgs, command...)
	dockerCmd := exec.Command("docker", dockerArgs...)
	dockerCmd.Stdin, dockerCmd.Stdout = os.Stdin, os.Stdout
	var stderr bytes.Buffer
	dockerCmd.Stderr = &stderr

	if debug {
		if len(config.Environment) > 0 {
			fmt.Fprintln(os.Stderr)
		}
		printfDebug(os.Stderr, "%s\n\n", strings.Join(dockerCmd.Args, " "))
	}

	if err := runCommands(config.RunBefore); err != nil {
		return -1
	}
	if err := dockerCmd.Run(); err != nil {
		if stderr.Len() > 0 {
			fmt.Fprintf(os.Stderr, errorString(stderr.String()))
			fmt.Fprintf(os.Stderr, "\n%s %s\n", dockerCmd.Args[0], strings.Join(dockerArgs, " "))

			if runtime.GOOS == "windows" {
				fmt.Fprintln(os.Stderr, windowsMessage)
			}
		}
	}
	if err := runCommands(config.RunAfter); err != nil {
		fmt.Fprintf(os.Stderr, errorString("%v", err))
	}

	return dockerCmd.ProcessState.Sys().(syscall.WaitStatus).ExitStatus()
}

var printfDebug = color.New(color.FgWhite, color.Faint).FprintfFunc()

func runCommands(commands []string) error {
	sort.Sort(sort.Reverse(sort.StringSlice(commands)))
	for _, script := range commands {
		cmd, tempFile, err := utils.GetCommandFromString(script)
		if err != nil {
			return err
		}
		if tempFile != "" {
			defer func() { os.Remove(tempFile) }()
		}
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return err
		}
	}
	return nil
}

// Returns the image name to use
// If docker-image-build option has been set, an image is dynamically built and the resulting image digest is returned
func getImage() string {
	if config.ImageBuild == "" && config.ImageBuildFolder == "" {
		return config.GetImageName()
	}

	if config.ImageBuildFolder == "" {
		config.ImageBuildFolder = "."
	}

	var dockerFile string
	if config.ImageBuild != "" {
		out := Must(ioutil.TempFile(config.ImageBuildFolder, "DockerFile")).(*os.File)
		Must(fmt.Fprintf(out, "FROM %s \n%s", config.GetImageName(), config.ImageBuild))
		Must(out.Close())
		defer os.Remove(out.Name())
		dockerFile = out.Name()
	}

	args := []string{"build", config.ImageBuildFolder, "--quiet", "--force-rm"}
	if refresh {
		args = append(args, "--pull")
	}
	if dockerFile != "" {
		args = append(args, "--file")
		args = append(args, dockerFile)
	}
	buildCmd := exec.Command("docker", args...)

	if debug {
		printfDebug(os.Stderr, "%s\n", strings.Join(buildCmd.Args, " "))
	}
	buildCmd.Stderr = os.Stderr
	buildCmd.Dir = config.ImageBuildFolder

	return strings.TrimSpace(string(Must(buildCmd.Output()).([]byte)))
}

func checkImage(image string) bool {
	var out bytes.Buffer
	dockerCmd := exec.Command("docker", []string{"images", "-q", image}...)
	dockerCmd.Stdout = &out
	dockerCmd.Run()
	return out.String() != ""
}

func refreshImage(image string) {
	fmt.Fprintf(os.Stderr, "Checking if there is a newer version of docker image %v\n", image)
	dockerUpdateCmd := exec.Command("docker", "pull", image)
	dockerUpdateCmd.Stdout, dockerUpdateCmd.Stderr = os.Stderr, os.Stderr
	err := dockerUpdateCmd.Run()
	PanicOnError(err)
	touchImageRefresh(image)
	fmt.Fprintln(os.Stderr)
}

func getEnviron(noHome bool) (result []string) {
	for _, env := range os.Environ() {
		split := strings.Split(env, "=")
		varName := strings.TrimSpace(split[0])
		varUpper := strings.ToUpper(varName)
		if varName == "" || strings.Contains(varUpper, "PATH") {
			continue
		}

		if runtime.GOOS == "windows" {
			if strings.Contains(strings.ToUpper(split[1]), `C:\`) || strings.Contains(varUpper, "WIN") {
				continue
			}
		}

		switch varName {
		case
			"_", "PWD", "OLDPWD", "TMPDIR",
			"PROMPT", "SHELL", "SH", "ZSH", "HOME",
			"LANG", "LC_CTYPE", "DISPLAY", "TERM":
		case "LOGNAME", "USER":
			if noHome {
				continue
			}
			fallthrough
		default:
			result = append(result, "-e")
			result = append(result, split[0])
		}
	}
	return
}

// This function set the path converter function
// For old Windows version still using docker-machine and VirtualBox,
// it transforms the C:\ to /C/.
func getPathConversionFunction() func(string) string {
	if runtime.GOOS != "windows" || os.Getenv("DOCKER_MACHINE_NAME") == "" {
		return func(path string) string { return path }
	}

	return func(path string) string {
		return fmt.Sprintf("/%s%s", strings.ToUpper(path[:1]), path[2:])
	}
}

var convertDrive = getPathConversionFunction()

var windowsMessage = `
You may have to share your drives with your Docker virtual machine to make them accessible.

On Windows 10+ using Hyper-V to run Docker, simply right click on Docker icon in your tray and
choose "Settings", then go to "Shared Drives" and enable the share for the drives you want to
be accessible to your dockers.

On previous version using VirtualBox, start the VirtualBox application and add shared drives
for all drives you want to make shareable with your dockers.

IMPORTANT, to make your drives accessible to tgf, you have to give them uppercase name corresponding
to the drive letter:
	C:\ ==> /C
	D:\ ==> /D
	...
	Z:\ ==> /Z
`
