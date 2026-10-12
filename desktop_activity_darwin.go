//go:build desktop && darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#import <Foundation/Foundation.h>

static void *px0_begin_activity(void) {
  @autoreleasepool {
    id a = [[NSProcessInfo processInfo]
        beginActivityWithOptions:NSActivityUserInitiatedAllowingIdleSystemSleep
                          reason:@"Indexing workspace"];
    return [a retain];
  }
}

static void px0_end_activity(void *a) {
  @autoreleasepool {
    [[NSProcessInfo processInfo] endActivity:(id)a];
    [(id)a release];
  }
}
*/
import "C"

// userActivity marks work the user is waiting on, so macOS does not throttle
// it (App Nap, background QoS) while the window is not frontmost. The
// returned func ends it.
func userActivity() func() {
	a := C.px0_begin_activity()
	return func() { C.px0_end_activity(a) }
}
