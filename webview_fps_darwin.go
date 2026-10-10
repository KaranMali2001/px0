//go:build desktop && darwin

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework Cocoa -framework WebKit
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <dispatch/dispatch.h>
#import <objc/message.h>

static void px0_unlockWebView(WKWebView *webView) {
  WKPreferences *preferences = webView.configuration.preferences;
  SEL featuresSelector = NSSelectorFromString(@"_features");
  SEL setSelector = NSSelectorFromString(@"_setEnabled:forFeature:");
  if (!preferences || ![(id)[WKPreferences class] respondsToSelector:featuresSelector] || ![preferences respondsToSelector:setSelector]) return;

  NSArray *features = ((id (*)(id, SEL))objc_msgSend)(WKPreferences.class, featuresSelector);
  for (id feature in features) {
    SEL keySelector = NSSelectorFromString(@"key");
    if (![feature respondsToSelector:keySelector]) continue;
    NSString *key = ((id (*)(id, SEL))objc_msgSend)(feature, keySelector);
    if (![key isEqualToString:@"PreferPageRenderingUpdatesNear60FPSEnabled"]) continue;
    ((void (*)(id, SEL, BOOL, id))objc_msgSend)(preferences, setSelector, NO, feature);
    return;
  }
}

static void px0_unlockWebViews(NSView *view) {
  if ([view isKindOfClass:WKWebView.class]) {
    px0_unlockWebView((WKWebView *)view);
    return;
  }
  for (NSView *child in view.subviews) px0_unlockWebViews(child);
}

static void px0_enableHighRefreshRate(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    for (NSWindow *window in NSApp.windows) px0_unlockWebViews(window.contentView);
  });
}
*/
import "C"

func enableHighRefreshRate() {
	C.px0_enableHighRefreshRate()
}
