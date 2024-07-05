use anyhow::Ok;
use clap::Parser;
use ipnetwork::{IpNetwork, Ipv4Network, Ipv6Network};
use log::info;
use std::{
    env,
    net::{IpAddr, Ipv4Addr},
    str::FromStr,
};

#[derive(Debug, Parser)]
pub struct Options {
    #[clap(long)]
    pub attach_devs: Vec<String>,

    #[clap(long)]
    pub svc_cidrs: Vec<String>,

    #[clap(default_value = "debug", long)]
    pub log: String,
}

#[tokio::main]
async fn main() -> Result<(), anyhow::Error> {
    let opts: Options = Options::parse();
    env::set_var(env_logger::DEFAULT_FILTER_ENV, opts.log);
    env_logger::Builder::new().parse_env("RUST_LOG").init();
    let attach_devs_str = opts.attach_devs;
    if attach_devs_str.len() == 0 {
        return Err(anyhow::anyhow!("attach_devs is empty"));
    }

    let cidrs: Vec<IpNetwork> = opts
        .svc_cidrs
        .iter()
        .filter_map(|s| IpNetwork::from_str(s).ok())
        .collect();
    if cidrs.len() == 0 {
        return Err(anyhow::anyhow!(
            "svc_cidrs {:?} not valid cidr",
            opts.svc_cidrs
        ));
    }
    info!("cluster cidr is {:?}", cidrs);

    let ip = IpAddr::V4(Ipv4Addr::new(192, 168, 1, 1));

    Ok(())
}
