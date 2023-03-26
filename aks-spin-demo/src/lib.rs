use anyhow::Result;
use spin_sdk::{
    http::{Request, Response},
    http_component,
};
use figlet_rs::FIGfont;

/// A simple Spin HTTP component.
#[http_component]
fn handle_aks_spin_demo(_: Request) -> Result<Response> {
    let standard_font = FIGfont::standard().unwrap();
    let figure = standard_font.convert("Hello, Fermyon on Azure AKS!");
    Ok(http::Response::builder()
        .status(200).body(Some(figure.unwrap().to_string().into()))?)
}
