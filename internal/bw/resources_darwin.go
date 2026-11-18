//go:build darwin && cgo

package bw

/*
#include <mach/mach.h>
#include <mach/mach_host.h>
static double bw_available_memory(void) {
 vm_statistics64_data_t stats;
 mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
 mach_port_t host = mach_host_self();
 kern_return_t result = host_statistics64(host, HOST_VM_INFO64, (host_info64_t)&stats, &count);
 vm_size_t page_size = 0;
 kern_return_t page_result = host_page_size(host, &page_size);
 mach_port_deallocate(mach_task_self(), host);
 if (result != KERN_SUCCESS || page_result != KERN_SUCCESS) return -1;
 return ((double)stats.free_count + stats.inactive_count + stats.purgeable_count) * page_size;
}
*/
import "C"
import "fmt"

func availableMemory() (float64, error) {
	n := float64(C.bw_available_memory())
	if n < 0 {
		return 0, fmt.Errorf("host_statistics64 failed")
	}
	return n, nil
}
