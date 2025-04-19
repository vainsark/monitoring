package ids

const (
	LoadID         int32 = 1
	LatencyID      int32 = 2
	AvailabilityID int32 = 3

	DeviceId   = "VM"
	CPUID      = 1
	MemoryID   = 2
	DiskID     = 3
	NetworkID  = 4
	SensoricID = 5

	StorageLinuxID = "sda"     // Linux (generic, can be changed based on the system
	StorageRaspID  = "mmcblk0" // Raspberry Pi (generic, can be changed based on the system

)
