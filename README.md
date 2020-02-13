# TGF

A **T**erra**g**runt **f**rontend that allow execution of Terragrunt/Terraform through Docker.

## README

This repo is just a fork (with some modifications) of the official [coveo TGF repository](https://github.com/coveooss/tgf). For instructions on how to use TGF, please consult the coveo README.

## NGC installation

The installation process is the same as coveo TGF but you will download the release from the nextgearcapital repository.

Choose the desired version according to your OS [here](https://github.com/nextgearcapital/tgf/releases), unzip it, make tgf executable `chmod +x tgf` and put it somewhere in your PATH.

or install it through command line:

On `OSX`:

```bash
curl -sL https://github.com/nextgearcapital/tgf/releases/download/v1.22.0-NGC/tgf_1.22.0-NGC_linux_64-bits.zip | bsdtar -xf- -C /usr/local/bin
```

On `Linux`:

```bash
curl -sL https://github.com/nextgearcapital/tgf/releases/download/v1.22.0-NGC/tgf_1.22.0-NGC_linux_64-bits.zip | gzip -d > /usr/local/bin/tgf && chmod +x /usr/local/bin/tgf
```

On `Windows` with Powershell:

```powershell
Invoke-WebRequest https://github.com/nextgearcapital/tgf/releases/download/v1.22.0-NGC/tgf_1.22.0-NGC_windows_64-bits.zip -OutFile tgf.zip
```
