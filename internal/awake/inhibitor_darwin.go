//go:build darwin && cgo

package awake

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation
#include <IOKit/pwr_mgt/IOPMLib.h>

static IOReturn acquireSleepAssertion(IOPMAssertionID *id) {
	return IOPMAssertionCreateWithName(
		kIOPMAssertionTypePreventUserIdleSystemSleep,
		kIOPMAssertionLevelOn,
		CFSTR("Codeoff Server app-server is running"),
		id
	);
}
*/
import "C"

import "fmt"

func acquire() (func() error, error) {
	var id C.IOPMAssertionID
	if result := C.acquireSleepAssertion(&id); result != C.kIOReturnSuccess {
		return nil, fmt.Errorf("create power assertion: IOKit error %d", result)
	}
	return func() error {
		if result := C.IOPMAssertionRelease(id); result != C.kIOReturnSuccess {
			return fmt.Errorf("release power assertion: IOKit error %d", result)
		}
		return nil
	}, nil
}
