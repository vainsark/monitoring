package ids

const (
	LoadID         int32 = 1
	LatencyID      int32 = 2
	AvailabilityID int32 = 3

	DeviceId = "VM"
	// DeviceId   = "Raspberry Pi 5"
	// DeviceId   = "Raspberry Pi 2"
	CPUID      = 1
	MemoryID   = 2
	DiskID     = 3
	NetworkID  = 4
	SensoricID = 5

	StorageLinuxID = "sda"     // Linux (generic, can be changed based on the system
	StorageRaspID  = "mmcblk0" // Raspberry Pi (generic, can be changed based on the system

	influxURL   = "http://localhost:8086"
	influxToken = "dvKsoUSbn-7vW04bNFdZeL87TNissgRP43i_ttrg-Vx3LdkzKJHucylmomEasS9an7lGv_TyZRj6-dHINMjXVA=="
	// influxToken  = "Ib2fq58MyBy2OUR9Aa3Lv2BN1uNBYnwMTsx4pyOSDzqoLZF6qKMTnfsB7hRO0_aFwxEOUPtbt3NUmyvs8RyhCw==" // Laptop
	influxOrg    = "vainsark"
	influxBucket = "metrics"
)
