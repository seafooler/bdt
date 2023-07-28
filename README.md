## Description
An implementation of Bolt-Dumbo Transformer in Golang, which is published in CCS'22 with the name ["Bolt-dumbo transformer: Asynchronous consensus as fast as the pipelined bft"](https://dl.acm.org/doi/pdf/10.1145/3548606.3559346)

## Usage
### 1. Machine types
Machines are divided into two types:
- *Workcomputer*: only used to configure `replicas` at the initial stage, particularly via `ansible` tool
- *Replicas*: run daemons of `BDT`, communicate with each other via P2P model

Only one workcomputer is needed, while the number of replicas is _n=3f+1_.
A machine can be a workcomputer and a replica simultaneously.

In the default configuration, we assume _f_=1, namely 4 replicas are needed.

### 2. Precondition
- Recommended OS releases: Ubuntu 18.04 (other releases may also be OK)
- Go version: 1.17+
- Python version: 3.6.9+

### 3. Steps to run BDT

#### 3.1 Install ansible on the workcomputer
Commands below are run on the *workcomputer*.
```shell script
sudo apt install python3-pip
sudo pip3 install --upgrade pip
pip3 install ansible
# add ~/.local/bin to your $PATH
echo 'export PATH=$PATH:~/.local/bin:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
```

#### 3.2 Login without passwords
Enable workcomputer to login in servers without passwords.

Commands below are run on the *work computer*.
```shell script
# Generate the ssh keys (if not generated before)
ssh-keygen
ssh-copy-id -i ~/.ssh/id_rsa.pub $IP_ADDR_OF_EACH_SERVER
```

#### 3.3 Generate configurations
Generate configurations for each server.

Commands below are run on the *workcomputer*.

- Change `id_ip`, `id_p2p_port`, and `id_name` in file `config_gen/config_template.yaml`
- Enter the directory `config_gen`, and run `go run main.go`

### 3.4 Build the executable file
Build the executable file on the workcomputer.
Enter the directory of `bdt`
```shell script
# Pull dependencies
go mod tidy
# Build
go build .
```

#### 3.5 Configure servers via Ansible tool
Change the `hosts` file in the directory `ansible`, the hostnames and IPs should be consistent with `config_gen/config_template.yaml`.
```shell script
# Enter the directory `ansible`
ansible-playbook conf-server.yaml -i hosts
```

#### 3.6 Run BDT servers via Ansible tool
```shell script
# clean the stale processes and testbed directories
ansible-playbook clean-server.yaml -i hosts
# run BDT servers
ansible-playbook run-server.yaml -i hosts
```
