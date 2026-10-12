//go:build desktop && darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

extern void px0MenuSettings(void);

@interface PX0SettingsTarget : NSObject
@end

@implementation PX0SettingsTarget
- (void)openSettings:(id)sender { px0MenuSettings(); }
@end

static const NSInteger px0SettingsTag = 0x70783073;

// Wails rebuilds the app menu on every update, so this re-runs and is idempotent.
static void px0_addSettingsItem(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    static PX0SettingsTarget *target;
    if (!target) target = [PX0SettingsTarget new];
    NSMenu *main = [NSApp mainMenu];
    if (main.numberOfItems == 0) return;
    NSMenu *app = [main itemAtIndex:0].submenu;
    if (!app || [app itemWithTag:px0SettingsTag]) return;
    NSMenuItem *item = [[[NSMenuItem alloc] initWithTitle:@"Settings…" action:@selector(openSettings:) keyEquivalent:@","] autorelease];
    item.target = target;
    item.tag = px0SettingsTag;
    NSInteger at = app.numberOfItems >= 2 && [app itemAtIndex:1].isSeparatorItem ? 2 : 0;
    [app insertItem:item atIndex:at];
    [app insertItem:[NSMenuItem separatorItem] atIndex:at + 1];
  });
}
*/
import "C"

func addSettingsMenuItem() {
	C.px0_addSettingsItem()
}
