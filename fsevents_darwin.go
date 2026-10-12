//go:build desktop && darwin

package main

/*
#cgo LDFLAGS: -framework CoreServices
#include <CoreServices/CoreServices.h>
#include <stdlib.h>
#include <dispatch/dispatch.h>

extern void px0FSEvents(uintptr_t handle, size_t n, char **paths, FSEventStreamEventFlags *flags);

static void px0_fsevents_cb(ConstFSEventStreamRef s, void *info, size_t n, void *paths,
                            const FSEventStreamEventFlags flags[], const FSEventStreamEventId ids[]) {
  px0FSEvents((uintptr_t)info, n, (char **)paths, (FSEventStreamEventFlags *)flags);
}

typedef struct {
  FSEventStreamRef stream;
  dispatch_queue_t queue;
} px0_fsevents;

static px0_fsevents *px0_fsevents_start(const char *root, uintptr_t handle, double latency) {
  CFStringRef path = CFStringCreateWithCString(NULL, root, kCFStringEncodingUTF8);
  if (!path) return NULL;
  CFArrayRef paths = CFArrayCreate(NULL, (const void **)&path, 1, &kCFTypeArrayCallBacks);
  FSEventStreamContext ctx = {0, (void *)handle, NULL, NULL, NULL};
  FSEventStreamRef stream = FSEventStreamCreate(NULL, px0_fsevents_cb, &ctx, paths,
      kFSEventStreamEventIdSinceNow, latency, kFSEventStreamCreateFlagNoDefer);
  CFRelease(paths);
  CFRelease(path);
  if (!stream) return NULL;
  px0_fsevents *w = malloc(sizeof *w);
  w->stream = stream;
  w->queue = dispatch_queue_create("ai.px0.fsevents", DISPATCH_QUEUE_SERIAL);
  FSEventStreamSetDispatchQueue(stream, w->queue);
  if (!FSEventStreamStart(stream)) {
    FSEventStreamInvalidate(stream);
    FSEventStreamRelease(stream);
    dispatch_release(w->queue);
    free(w);
    return NULL;
  }
  return w;
}

static void px0_fsevents_stop(px0_fsevents *w) {
  FSEventStreamStop(w->stream);
  FSEventStreamInvalidate(w->stream);
  FSEventStreamRelease(w->stream);
  // Invalidate does not wait for an in-flight callback; draining the queue
  // does, so the Go handle is never used after it is deleted.
  dispatch_sync(w->queue, ^{});
  dispatch_release(w->queue);
  free(w);
}
*/
import "C"

import (
	"errors"
	"path/filepath"
	"runtime/cgo"
	"strings"
	"unsafe"
)

func init() { startTreeEvents = fsTreeEvents }

// fsEventsLatency is how long FSEvents coalesces before calling back.
const fsEventsLatency = 0.1

type fsEventSink struct {
	root string // real path: FSEvents reports resolved paths (/private/tmp, not /tmp)
	out  chan treeEvent
}

func fsTreeEvents(root string) (<-chan treeEvent, func(), error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, err
	}
	sink := &fsEventSink{root: real, out: make(chan treeEvent, 1)}
	h := cgo.NewHandle(sink)
	croot := C.CString(real)
	defer C.free(unsafe.Pointer(croot))
	w := C.px0_fsevents_start(croot, C.uintptr_t(h), C.double(fsEventsLatency))
	if w == nil {
		h.Delete()
		return nil, nil, errors.New("fsevents: could not start stream")
	}
	stop := func() {
		C.px0_fsevents_stop(w)
		h.Delete()
	}
	return sink.out, stop, nil
}

//export px0FSEvents
func px0FSEvents(handle C.uintptr_t, n C.size_t, paths **C.char, flags *C.FSEventStreamEventFlags) {
	sink := cgo.Handle(handle).Value().(*fsEventSink)
	ps := unsafe.Slice(paths, int(n))
	fs := unsafe.Slice(flags, int(n))
	var ev treeEvent
	for i := range ps {
		if fs[i]&(C.kFSEventStreamEventFlagMustScanSubDirs|C.kFSEventStreamEventFlagRootChanged) != 0 {
			ev.Rescan = true
		}
		rel, ok := relUnder(sink.root, strings.TrimSuffix(C.GoString(ps[i]), "/"))
		if !ok {
			continue
		}
		ev.Dirs = append(ev.Dirs, rel)
	}
	sink.send(ev)
}

// send merges into a pending, unread event so a burst never blocks the
// FSEvents queue.
func (s *fsEventSink) send(ev treeEvent) {
	for {
		select {
		case s.out <- ev:
			return
		case old := <-s.out:
			ev.Rescan = ev.Rescan || old.Rescan
			ev.Dirs = append(old.Dirs, ev.Dirs...)
		}
	}
}

func relUnder(root, p string) (string, bool) {
	if p == root {
		return "", true
	}
	if !strings.HasPrefix(p, root+"/") {
		return "", false
	}
	return p[len(root)+1:], true
}
