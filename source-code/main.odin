package main

import "core:fmt"
import "core:os"
import "core:strings"
import "core:slice"
import "core:sort"
import "core:mem"
import "core:bufio"
import "core:io"
import "core:path/filepath"

main :: proc() {
    if len(os.args) < 2 {
        fmt.println("Usage: chker <command>")
        fmt.println("Commands:")
        fmt.println("  liquorix   - Remove current Debian kernel, install Liquorix kernel, set as default")
        fmt.println("  xanmod     - Remove current Debian kernel, install XanMod kernel")
        fmt.println("  deb-kernel - Remove current kernel (if not Debian), install Debian kernel")
        os.exit(1)
    }

    command := os.args[1]

    switch command {
    case "liquorix":
        install_liquorix()
    case "xanmod":
        install_xanmod()
    case "deb-kernel":
        install_deb_kernel()
    case:
        fmt.eprintln("Unknown command:", command)
        os.exit(1)
    }
}

install_xanmod :: proc() {
    log_prefix := "[xanmod-installer]"

    fmt.printf("%s Start\n", log_prefix)

    tmpfile := "/tmp/xanmod-cpu.hacker"
    github_raw := "https://raw.githubusercontent.com/HackerOS-Linux-System/Hacker-Lang/main/hacker-packages/xanmod-cpu.hacker"

    fmt.printf("%s Pobieram plik z GitHub: %s -> %s\n", log_prefix, github_raw, tmpfile)

    if !command_exists("curl") {
        fmt.printf("%s curl nieznalezione, instaluję curl...\n", log_prefix)
        run_sudo_command("apt", "update")
        run_sudo_command("apt", "install", "-y", "curl")
    }

    if !run_command("curl", "-fsSL", "-o", tmpfile, github_raw) {
        fmt.printf("%s Błąd pobierania pliku z GitHub. Sprawdź połączenie i URL.\n", log_prefix)
        os.exit(1)
    }

    fmt.printf("%s Plik pobrany.\n", log_prefix)

    // Parse patterns
    mappings, err := parse_mappings(tmpfile)
    if err != nil {
        fmt.printf("%s Error parsing mappings: %v\n", log_prefix, err)
        os.exit(1)
    }

    if len(mappings) == 0 {
        fmt.printf("%s Nie znaleziono poprawnych mapowań w pliku. Kończę.\n", log_prefix)
        os.exit(1)
    }

    patterns, targets := sort_mappings_by_length(mappings)

    // CPU INFO
    cpu_text := get_cpu_text()
    fmt.printf("%s Wykryty CPU:\n", log_prefix)
    fmt.printf("%s %s\n", log_prefix, strings.trim_space(get_cpu_model()))

    // Pattern match
    selected_target := ""
    selected_pattern := ""
    for i in 0..<len(patterns) {
        pat_lc := strings.to_lower(strings.trim_space(patterns[i]))
        if len(pat_lc) > 0 && strings.contains(cpu_text, pat_lc) {
            selected_target = targets[i]
            selected_pattern = patterns[i]
            break
        }
    }

    if selected_target == "" {
        for i in 0..<len(patterns) {
            if strings.contains(strings.to_lower(patterns[i]), "all x86-64") {
                selected_target = targets[i]
                selected_pattern = patterns[i]
                break
            }
        }
    }

    if selected_target == "" {
        fmt.printf("%s Brak dopasowania — używam domyślnego x86-64\n", log_prefix)
        selected_target = "x86-64"
        selected_pattern = "(default x86-64)"
    }

    fmt.printf("%s Dopasowano: '%s' -> '%s'\n", log_prefix, selected_pattern, selected_target)

    // Mapowanie wariantu
    xanmod_variant := "x64v1"
    if strings.contains(selected_target, "v3") {
        xanmod_variant = "x64v3"
    } else if strings.contains(selected_target, "v2") {
        xanmod_variant = "x64v2"
    }

    fmt.printf("%s Wybrana wersja xanmod: %s\n", log_prefix, xanmod_variant)

    // Instalacja repozytorium i pakietu
    add_xanmod_repo(log_prefix)
    pkg := fmt.tprintf("linux-xanmod-lts-%s", xanmod_variant)
    fmt.printf("%s Instaluję: %s\n", log_prefix, pkg)
    run_sudo_command("apt", "install", "-y", pkg)

    fmt.printf("%s Instalacja kernela xanmod zakończona (jeśli nie było błędów).\n", log_prefix)

    // NVIDIA
    check_and_install_nvidia(log_prefix)

    // Usuwanie debian kernel
    remove_debian_kernel(log_prefix)

    // Config file
    create_config_file("xanmod")

    // Update GRUB
    fmt.printf("%s Aktualizuję GRUB...\n", log_prefix)
    run_sudo_command("update-grub")

    fmt.printf("%s GOTOWE. Zrestartuj system, aby uruchomić nowe jądro.\n", log_prefix)
}

install_liquorix :: proc() {
    log_prefix := "[liquorix-installer]"

    fmt.printf("%s Start\n", log_prefix)

    tmpfile := "/tmp/xanmod-cpu.hacker"
    github_raw := "https://raw.githubusercontent.com/HackerOS-Linux-System/Hacker-Lang/main/hacker-packages/xanmod-cpu.hacker"

    fmt.printf("%s Pobieram plik z GitHub: %s -> %s\n", log_prefix, github_raw, tmpfile)

    if !command_exists("curl") {
        fmt.printf("%s curl nie znalezione, instaluję curl...\n", log_prefix)
        run_sudo_command("apt", "update")
        run_sudo_command("apt", "install", "-y", "curl")
    }

    if !run_command("curl", "-fsSL", "-o", tmpfile, github_raw) {
        fmt.printf("%s Błąd pobierania pliku z GitHub.\n", log_prefix)
        os.exit(1)
    }

    fmt.printf("%s Plik pobrany.\n", log_prefix)

    // Parse patterns
    mappings, err := parse_mappings(tmpfile)
    if err != nil {
        fmt.printf("%s Error parsing mappings: %v\n", log_prefix, err)
        os.exit(1)
    }

    if len(mappings) == 0 {
        fmt.printf("%s Brak poprawnych mapowań.\n", log_prefix)
        os.exit(1)
    }

    patterns, targets := sort_mappings_by_length(mappings)

    // CPU INFO
    cpu_text := get_cpu_text()
    fmt.printf("%s Wykryty CPU:\n", log_prefix)
    fmt.printf("%s %s\n", log_prefix, strings.trim_space(get_cpu_model()))

    // Pattern match
    selected_target := ""
    selected_pattern := ""
    for i in 0..<len(patterns) {
        pat_lc := strings.to_lower(strings.trim_space(patterns[i]))
        if len(pat_lc) > 0 && strings.contains(cpu_text, pat_lc) {
            selected_target = targets[i]
            selected_pattern = patterns[i]
            break
        }
    }

    if selected_target == "" {
        for i in 0..<len(patterns) {
            if strings.contains(strings.to_lower(patterns[i]), "all x86-64") {
                selected_target = targets[i]
                selected_pattern = patterns[i]
                break
            }
        }
    }

    if selected_target == "" {
        fmt.printf("%s Brak dopasowania — używam domyślnego x86-64\n", log_prefix)
        selected_target = "x86-64"
        selected_pattern = "(default x86-64)"
    }

    fmt.printf("%s Dopasowano: '%s' -> '%s'\n", log_prefix, selected_pattern, selected_target)

    // Wariant CPU
    cpu_variant := "x64v1"
    if strings.contains(selected_target, "v3") {
        cpu_variant = "x64v3"
    } else if strings.contains(selected_target, "v2") {
        cpu_variant = "x64v2"
    }

    fmt.printf("%s Wariant CPU: %s\n", log_prefix, cpu_variant)

    // Instalacja Liquorix
    fmt.printf("%s Instaluję jądro Liquorix...\n", log_prefix)
    if !run_command("curl", "-s", "https://liquorix.net/install-liquorix.sh") {
        fmt.printf("%s Błąd pobierania skryptu Liquorix!\n", log_prefix)
        os.exit(1)
    }
    // Zakładamy, że run_sudo_bash_pipe obsługuje pipe do bash
    // Ale w Odin, musimy to obsłużyć inaczej. Pobierz do temp i uruchom.
    liquorix_script := "/tmp/install-liquorix.sh"
    run_command("curl", "-s", "https://liquorix.net/install-liquorix.sh", "-o", liquorix_script)
    run_sudo_command("bash", liquorix_script)

    fmt.printf("%s Jądro Liquorix zainstalowane.\n", log_prefix)

    // NVIDIA
    check_and_install_nvidia(log_prefix)

    // Usuwanie debian kernel
    remove_debian_kernel(log_prefix)

    // Config file
    create_config_file("liquorix")

    fmt.printf("%s GOTOWE. Zrestartuj system, aby włączyć jądro Liquorix.\n", log_prefix)
}

install_deb_kernel :: proc() {
    log_prefix := "[deb-kernel-installer]"

    // Sprawdź czy debian kernel nie jest zainstalowany
    current_kernel := get_current_kernel()
    if strings.contains(current_kernel, "debian") || !strings.contains(current_kernel, "liquorix") && !strings.contains(current_kernel, "xanmod") {
        fmt.printf("%s Jądro Debiana jest już zainstalowane lub nie ma alternatywnego. Nic nie robię.\n", log_prefix)
        return
    }

    fmt.printf("%s Usuwam aktualne jądro i instaluję debianowe.\n", log_prefix)

    // Usuń aktualne (liquorix lub xanmod)
    remove_current_kernel(log_prefix)

    // Instaluj debian kernel - zakładamy default apt install linux-image-amd64 lub podobny
    run_sudo_command("apt", "update")
    run_sudo_command("apt", "install", "-y", "linux-image-amd64")

    // Update GRUB
    run_sudo_command("update-grub")

    // Usuń config file jeśli istnieje
    config_file := filepath.join(os.get_env("HOME"), ".config", "hackeros", "kernel.hacker")
    os.remove(config_file)

    fmt.printf("%s GOTOWE. Zrestartuj system.\n", log_prefix)
}

remove_current_kernel :: proc(log_prefix: string) {
    current_uname := run_command_output("uname", "-r")
    current_uname = strings.trim_space(current_uname)
    pkg := fmt.tprintf("linux-image-%s", current_uname)
    run_sudo_command("apt", "remove", "--purge", "-y", pkg)
}

remove_debian_kernel :: proc(log_prefix: string) {
    remove_script := "/usr/share/HackerOS/Scripts/Bin/remove-debian-kernel.sh"
    if os.exists(remove_script) {
        fmt.printf("%s Uruchamiam skrypt usuwający stare jądra: %s\n", log_prefix, remove_script)
        run_sudo_command(remove_script)
    } else {
        fmt.printf("%s Skrypt do usuwania debianowych jąder nie istnieje: %s\n", log_prefix, remove_script)
        // Fallback: remove default debian kernel
        remove_current_kernel(log_prefix)
    }
}

create_config_file :: proc(kernel_type: string) {
    config_dir := filepath.join(os.get_env("HOME"), ".config", "hackeros")
    os.make_directory(config_dir, 0o755)
    config_file := filepath.join(config_dir, "kernel.hacker")
    data := fmt.tprintf("[%s]\n", kernel_type)
    os.write_entire_file(config_file, transmute([]byte)data)
    os.set_file_mode(config_file, 0o644)
}

add_xanmod_repo :: proc(log_prefix: string) {
    fmt.printf("%s Dodaję repozytorium xanmod...\n", log_prefix)
    run_sudo_command("mkdir", "-p", "/etc/apt/keyrings")
    if !command_exists("wget") {
        fmt.printf("%s wget nieznalezione, instaluję wget...\n", log_prefix)
        run_sudo_command("apt", "update")
        run_sudo_command("apt", "install", "-y", "wget")
    }
    run_command("wget", "-qO", "-", "https://dl.xanmod.org/archive.key")
    // Pipe to gpg
    // W Odin, użyj process do pipe
    // Dla prostoty, pobierz do temp i przetwórz
    key_temp := "/tmp/xanmod.key"
    run_command("wget", "-qO", key_temp, "https://dl.xanmod.org/archive.key")
    run_sudo_command("gpg", "--dearmor", "-o", "/etc/apt/keyrings/xanmod-archive-keyring.gpg", key_temp)

    release := run_command_output("lsb_release", "-sc")
    release = strings.trim_space(release)
    list_content := fmt.tprintf("deb [signed-by=/etc/apt/keyrings/xanmod-archive-keyring.gpg] http://deb.xanmod.org %s main\n", release)
    list_file := "/etc/apt/sources.list.d/xanmod-release.list"
    os.write_entire_file(list_file, transmute([]byte)list_content)
    run_sudo_command("apt", "update")
}

check_and_install_nvidia :: proc(log_prefix: string) {
    fmt.printf("%s Sprawdzam NVIDIA...\n", log_prefix)
    has_nvidia := false

    if !command_exists("lspci") {
        fmt.printf("%s Brak lspci. Instaluję pciutils...\n", log_prefix)
        run_sudo_command("apt", "update")
        run_sudo_command("apt", "install", "-y", "pciutils")
    }

    lspci_output := run_command_output("lspci", "-nnk")
    if strings.contains(strings.to_lower(lspci_output), "nvidia") {
        has_nvidia = true
    }

    if has_nvidia {
        fmt.printf("%s Wykryto NVIDIA — instaluję sterowniki.\n", log_prefix)
        run_sudo_command("apt", "update")
        run_sudo_command("apt", "install", "-y", "nvidia-driver", "nvidia-kernel-dkms", "nvidia-smi", "libnvidia-ml1", "nvidia-settings", "nvidia-cuda-mps")
    } else {
        fmt.printf("%s Nie wykryto NVIDIA.\n", log_prefix)
    }
}

parse_mappings :: proc(filename: string) -> ([]string, os.Error) {
    file, err := os.open(filename)
    if err != nil { return nil, err }
    defer os.close(file)

    mappings: [dynamic]string
    defer delete(mappings)

    scanner: bufio.Scanner
    bufio.scanner_init(&scanner, os.stream_from_handle(file))
    defer bufio.scanner_destroy(&scanner)

    for bufio.scanner_scan(&scanner) {
        line := bufio.scanner_text(&scanner)
        line = strings.trim_space(line)
        if len(line) == 0 || line[0] == '#' { continue }
        if strings.has_prefix(line, "[") && strings.has_suffix(line, "]") {
            inner := line[1:len(line)-1]
            if strings.contains(inner, ">") {
                append(&mappings, strings.clone(inner))
            }
        }
    }

    return slice.clone(mappings[:]), nil
}

sort_mappings_by_length :: proc(mappings: []string) -> ([]string, []string) {
    type Pair = struct { pat: string, tar: string, len: int }
    pairs: [dynamic]Pair
    defer delete(pairs)

    for m in mappings {
        parts := strings.split(m, ">")
        if len(parts) == 2 {
            pat := strings.trim_space(parts[0])
            tar := strings.trim_space(parts[1])
            append(&pairs, Pair{pat, tar, len(pat)})
        }
    }

    sort.quick_sort_proc(pairs[:], proc(a, b: Pair) -> int {
        return b.len - a.len // Descending
    })

    patterns := make([]string, len(pairs))
    targets := make([]string, len(pairs))
    for p, i in pairs {
        patterns[i] = p.pat
        targets[i] = p.tar
    }

    return patterns, targets
}

get_cpu_text :: proc() -> string {
    cpu_info := run_command_output("lscpu")
    cpu_model := get_cpu_model()
    cpu_vendor := get_cpu_vendor()
    cpu_text := fmt.tprintf("%s %s %s", cpu_model, cpu_vendor, cpu_info)
    return strings.to_lower(strings.trim_space(cpu_text))
}

get_cpu_model :: proc() -> string {
    content := read_file("/proc/cpuinfo")
    for line in strings.split_lines_iterator(&content) {
        if strings.has_prefix(line, "Model name:") {
            parts := strings.split(line, ":")
            if len(parts) > 1 {
                return strings.trim_space(parts[1])
            }
        }
    }
    return ""
}

get_cpu_vendor :: proc() -> string {
    content := read_file("/proc/cpuinfo")
    for line in strings.split_lines_iterator(&content) {
        if strings.has_prefix(line, "Vendor ID:") || strings.has_prefix(line, "vendor_id:") {
            parts := strings.split(line, ":")
            if len(parts) > 1 {
                return strings.trim_space(parts[1])
            }
        }
    }
    return ""
}

get_current_kernel :: proc() -> string {
    return run_command_output("uname", "-r")
}

read_file :: proc(path: string) -> string {
    data, ok := os.read_entire_file(path)
    if !ok { return "" }
    return string(data)
}

command_exists :: proc(cmd: string) -> bool {
    _, code := os.exec_command(fmt.tprintf("command -v %s", cmd))
    return code == 0
}

run_command :: proc(args: ..string) -> bool {
    _, code := os.exec_command(strings.join(args, " "))
    return code == 0
}

run_sudo_command :: proc(args: ..string) -> bool {
    sudo_args := make([dynamic]string, context.allocator)
    append(&sudo_args, "sudo")
    for arg in args {
        append(&sudo_args, arg)
    }
    _, code := os.exec_command(strings.join(sudo_args[:], " "))
    return code == 0
}

run_command_output :: proc(args: ..string) -> string {
    output, code := os.exec_command(strings.join(args, " "))
    if code != 0 { return "" }
    return strings.trim_space(output)
}
