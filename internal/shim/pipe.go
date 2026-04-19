package shim

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/sivakumar455/memcp/internal/observation"
)

// lineScanner scans line-by-line without consuming the actual byte stream for the next reader.
func sniffStreamAsnc(r io.Reader, callback func([]byte)) {
	go func() {
		scanner := bufio.NewScanner(r)
		// increase buffer if large MCP responses
		buf := make([]byte, 0, 64*1024)
		scanner.Buffer(buf, 10*1024*1024)

		for scanner.Scan() {
			// Pass a copy of the bytes to avoid race conditions with scanner internal buffers
			b := make([]byte, len(scanner.Bytes()))
			copy(b, scanner.Bytes())
			
			go callback(b) // non-blocking execution of regex
		}
	}()
}

// RunShim starts the backend MCP server process, pipes its standard I/O to the current process, 
// and connects a Sniffer in the middle.
func RunShim(cmdLine []string, observer *observation.Observer, backendName string) error {
	if len(cmdLine) == 0 {
		return fmt.Errorf("no backend command provided")
	}

	sniffer := NewSniffer(observer, backendName)

	backendCommand := exec.Command(cmdLine[0], cmdLine[1:]...)
	
	// Create pipe ends
	cmdStdinReader, cmdStdinWriter := io.Pipe()
	cmdStdoutReader, cmdStdoutWriter := io.Pipe()
	cmdStderrReader, cmdStderrWriter := io.Pipe()

	backendCommand.Stdin = cmdStdinReader
	backendCommand.Stdout = cmdStdoutWriter
	backendCommand.Stderr = cmdStderrWriter

	// Start the Command
	if err := backendCommand.Start(); err != nil {
		return fmt.Errorf("starting backend process: %w", err)
	}

	var wg sync.WaitGroup

	// Tee the OS.Stdin -> to cmdStdinWriter and to our sniffer
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cmdStdinWriter.Close() // notify backend that stdin is closed if IDE disconnects
		pr, pw := io.Pipe()
		tee := io.TeeReader(os.Stdin, pw)
		
		sniffStreamAsnc(pr, sniffer.SniffRequest)
		_, _ = io.Copy(cmdStdinWriter, tee)
	}()

	// Tee the cmdStdoutReader -> OS.Stdout and to our sniffer
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cmdStdoutReader.Close()
		pr, pw := io.Pipe()
		tee := io.TeeReader(cmdStdoutReader, pw)

		sniffStreamAsnc(pr, sniffer.SniffResponse)
		_, _ = io.Copy(os.Stdout, tee)
	}()

	// Just pass Stderr directly
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cmdStderrReader.Close()
		_, _ = io.Copy(os.Stderr, cmdStderrReader)
	}()

	err := backendCommand.Wait()
	
	// Ensure observer async jobs format nicely before shutdown
	observer.Wait()
	
	return err
}
