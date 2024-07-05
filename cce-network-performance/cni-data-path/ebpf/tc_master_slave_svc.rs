#![no_std]
#![no_main]
use aya_ebpf::{
    bindings::{BPF_F_INGRESS, TC_ACT_PIPE, TC_ACT_SHOT},
    helpers,
    macros::{classifier, map},
    maps::{Array, HashMap},
    programs::TcContext,
};
use aya_log_ebpf::info;
use cce_cni_common::ipnet::IpNetwork;
use core::net::Ipv4Addr;
use network_types::{
    eth::{EthHdr, EtherType},
    ip::Ipv4Hdr,
};

#[map]
static SERVICE_CIDR_ARRAY: Array<IpNetwork> = Array::pinned(2, 0);

#[map]
static LOCAL_MASTER_SLAVE_DEVICES: HashMap<u32, u32> = HashMap::with_max_entries(1024, 0);

#[classifier]
pub fn tc_egress(ctx: TcContext) -> i32 {
    match try_tc_egress(ctx) {
        Ok(ret) => ret,
        Err(_) => TC_ACT_SHOT,
    }
}

fn try_tc_egress(ctx: TcContext) -> Result<i32, ()> {
    let ethhdr: EthHdr = ctx.load(0).map_err(|_| ())?;
    match ethhdr.ether_type {
        EtherType::Ipv4 => {}
        _ => return Ok(TC_ACT_PIPE),
    }

    let ipv4hdr: Ipv4Hdr = ctx.load(EthHdr::LEN).map_err(|_| ())?;

    // let destination = u32::from_be(ipv4hdr.dst_addr);
    let destination = Ipv4Addr::from(ipv4hdr.dst_addr);
    let action = if is_to_svc_v4(&destination) {
        if let Some(&slave) = get_ifindex(&ctx.skb) {
            unsafe { helpers::bpf_redirect(slave, BPF_F_INGRESS as u64) as i32 }
        } else {
            TC_ACT_PIPE
        }
    } else {
        TC_ACT_PIPE
    };

    // info!(&ctx, "DEST {:?}, ACTION {}", destination, action);
    info!(&ctx, "ACTION {}", action);

    Ok(action)
}

// to_svc_v4
fn is_to_svc_v4(address: &Ipv4Addr) -> bool {
    if let Some(svc_cidr) = SERVICE_CIDR_ARRAY.get_ptr(0) {
        // if !svc_cidr.valid() {
        //     return false;
        // }
        unsafe {
            if let IpNetwork::V4(cidr_ip) = *svc_cidr {
                return cidr_ip.contains(*address);
            }
        }
    }
    false
}

// get slave ifindex from sk_buff
fn get_ifindex(sk_buff: &aya_ebpf::programs::sk_buff::SkBuff) -> Option<&u32> {
    unsafe {
        let index = helpers::bpf_probe_read_kernel(&(*(sk_buff.skb)).ifindex as *const u32)
            .map_or(0, |x| x);
        if index > 0 {
            return LOCAL_MASTER_SLAVE_DEVICES.get(&index);
        }
    }

    None
}

#[cfg(not(test))]
#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    unsafe { core::hint::unreachable_unchecked() }
}
