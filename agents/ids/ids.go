package ids

const (
	LoadID         int32 = 1
	LatencyID      int32 = 2
	AvailabilityID int32 = 3

	DeviceId = "VM"
	// DeviceId   = "Raspberry_Pi_5"
	// DeviceId   = "Raspberry_Pi_2"

	ServerIP = "localhost"

	CPUID      = 1
	MemoryID   = 2
	DiskID     = 3
	NetworkID  = 4
	SensoricID = 5

	StorageID = "sda" // Linux (generic, can be changed based on the system
	// StorageID = "mmcblk0" // Raspberry Pi (generic, can be changed based on the system

	InfluxURL   = "http://localhost:8086"
	InfluxToken = "dvKsoUSbn-7vW04bNFdZeL87TNissgRP43i_ttrg-Vx3LdkzKJHucylmomEasS9an7lGv_TyZRj6-dHINMjXVA=="
	// influxToken  = "Ib2fq58MyBy2OUR9Aa3Lv2BN1uNBYnwMTsx4pyOSDzqoLZF6qKMTnfsB7hRO0_aFwxEOUPtbt3NUmyvs8RyhCw==" // Laptop
	InfluxOrg    = "vainsark"
	InfluxBucket = "metrics"
)
