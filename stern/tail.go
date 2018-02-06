//   Copyright 2016 Wercker Holding BV
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

package stern

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"sync"

	"github.com/fatih/color"
	"github.com/pkg/errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

type Tail struct {
	Namespace      string
	PodName        string
	ContainerName  string
	Options        *TailOptions
	req            *rest.Request
	closed         chan struct{}
	podColor       *color.Color
	containerColor *color.Color
}

type TailOptions struct {
	Timestamps   bool
	SinceSeconds int64
	Exclude      []*regexp.Regexp
	Namespace    bool
	TailLines    *int64
	Writer       io.Writer
	WriterMutex  *sync.Mutex
}

// NewTail returns a new tail for a Kubernetes container inside a pod
func NewTail(namespace, podName, containerName string, options *TailOptions) *Tail {
	return &Tail{
		Namespace:     namespace,
		PodName:       podName,
		ContainerName: containerName,
		Options:       options,
		closed:        make(chan struct{}),
	}
}

var index = 0

var colorList = [][2]*color.Color{
	{color.New(color.FgHiCyan), color.New(color.FgCyan)},
	{color.New(color.FgHiGreen), color.New(color.FgGreen)},
	{color.New(color.FgHiMagenta), color.New(color.FgMagenta)},
	{color.New(color.FgHiYellow), color.New(color.FgYellow)},
	{color.New(color.FgHiBlue), color.New(color.FgBlue)},
	{color.New(color.FgHiRed), color.New(color.FgRed)},
}

var podColors = make(map[string]*color.Color)

// Start starts tailing
func (t *Tail) Start(ctx context.Context, i v1.PodInterface) {
	colorIndex := len(podColors) % len(colorList)
	podColor, ok := podColors[t.PodName]
	if !ok {
		podColor = colorList[colorIndex][0]
		podColors[t.PodName] = podColor
	}
	t.podColor = podColor
	t.containerColor = colorList[colorIndex][1]

	go func() {
		g := color.New(color.FgHiGreen, color.Bold).SprintFunc()
		p := t.podColor.SprintFunc()
		c := t.containerColor.SprintFunc()
		var str string
		if t.Options.Namespace {
			str = fmt.Sprintf(
				"%s %s %s › %s\n",
				g("+"), p(t.Namespace), p(t.PodName), c(t.ContainerName),
			)
		} else {
			str = fmt.Sprintf("%s %s › %s\n", g("+"), p(t.PodName), c(t.ContainerName))
		}
		t.PrintSafe(str)

		req := i.GetLogs(t.PodName, &corev1.PodLogOptions{
			Follow:       true,
			Timestamps:   t.Options.Timestamps,
			Container:    t.ContainerName,
			SinceSeconds: &t.Options.SinceSeconds,
			TailLines:    t.Options.TailLines,
		})

		stream, err := req.Stream()
		if err != nil {
			fmt.Println(errors.Wrapf(err, "Error opening stream to %s/%s: %s\n", t.Namespace, t.PodName, t.ContainerName))
			return
		}
		defer stream.Close()

		go func() {
			<-t.closed
			stream.Close()
		}()

		reader := bufio.NewReader(stream)

	OUTER:
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				return
			}

			str := string(line)

			for _, rex := range t.Options.Exclude {
				if rex.MatchString(str) {
					continue OUTER
				}
			}

			t.Print(str)
		}
	}()

	go func() {
		<-ctx.Done()
	}()
}

// Close stops tailing
func (t *Tail) Close() {
	r := color.New(color.FgHiRed, color.Bold).SprintFunc()
	p := t.podColor.SprintFunc()
	var str string
	if t.Options.Namespace {
		str = fmt.Sprintf("%s %s %s\n", r("-"), p(t.Namespace), p(t.PodName))
	} else {
		str = fmt.Sprintf("%s %s\n", r("-"), p(t.PodName))
	}
	t.PrintSafe(str)
	close(t.closed)
}

// Print prints a color coded log message with the pod and container names
func (t *Tail) Print(msg string) {
	p := t.podColor.SprintFunc()
	c := t.containerColor.SprintFunc()
	str := fmt.Sprintf("%s %s %s", p(t.PodName), c(t.ContainerName), msg)
	if t.Options.Namespace {
		str = fmt.Sprintf("%s %s", p(t.Namespace), str)
	}
	t.PrintSafe(str)
}

func (t *Tail) PrintSafe(str string) {
	t.Options.WriterMutex.Lock()
	defer t.Options.WriterMutex.Unlock()

	fmt.Fprintf(t.Options.Writer, str)
}
