package arch

type CPU string

const (
	X86_64 CPU = "x86_64"
	I386   CPU = "i386"
	ARM64  CPU = "aarch64"
)

// Architecture ... Provides values that are specific to each architecture
type Architecture struct {
	Name     string
	FullCPU  CPU
	ShortCPU string
	CPUArch  string
}

// X86_64_EFI ... x86_64 with UEFI, supported on Nitro
var X86_64_EFI = Architecture{Name: "x86_64", FullCPU: X86_64, ShortCPU: "x64", CPUArch: "amd64"}

// ARM64_EFI ... ARM64 target
var ARM64_EFI = Architecture{Name: "arm64", FullCPU: ARM64, ShortCPU: "aa64", CPUArch: "arm64"}
