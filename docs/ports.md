# Opening ports 80 and 443

Tero needs TCP ports 80 and 443 open to the whole internet:

- **443** serves the dashboard and your apps over HTTPS, and answers Let's Encrypt's TLS-ALPN-01 challenge ([RFC 8737](https://www.rfc-editor.org/rfc/rfc8737)).
- **80** answers Let's Encrypt's HTTP-01 challenge ([RFC 8555 §8.3](https://www.rfc-editor.org/rfc/rfc8555#section-8.3)) and redirects plain HTTP to HTTPS.

Let's Encrypt checks from several places on the internet, so these ports must be open to every address (`0.0.0.0/0`), not only to yours. Tero's own firewall on the server already opens them. Many cloud providers also have a firewall in front of the server that blocks them by default: AWS, Google Cloud, Azure and Oracle Cloud all do.

## How `sudo tero init` checks

Once Tero is running, `sudo tero init` asks [ifconfig.co](https://ifconfig.co) (the open-source [echoip](https://github.com/mpolden/echoip)), one port after the other, to connect back to the server's public IPv4 address on ports 80 and 443. When both are open it prints nothing about them. When one is closed it prints a ❌ line for it and the steps for your provider below. When both are closed, init doesn't wait for a certificate, since none can be issued. When the check can't run, for example because outbound traffic leaves through a different address than the server's public IP, it says the ports weren't checked. The check never stops init.

ifconfig.co sees your server's address, which Let's Encrypt's public certificate logs list anyway for `<ip>.nip.io` addresses.

Output is colored only on a terminal, and never when `NO_COLOR` is set. `sudo` drops most environment variables, so pass it through: `sudo NO_COLOR=1 tero init`.

After opening the ports, reload the setup link. Tero keeps retrying the certificate on its own, waiting longer after each failure, so it can take up to half an hour. To retry right away, run `sudo systemctl restart tero`. The setup link stays valid for 24 hours.

## AWS (EC2)

New instances get a security group that only lets in SSH, or the VPC's default security group, which only lets in traffic from itself ([security groups](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-security-groups.html), [default security group](https://docs.aws.amazon.com/vpc/latest/userguide/default-security-group.html)).

1. In the EC2 console, open **Instances**, select the server and open the **Security** tab.
2. Click its security group, then **Edit inbound rules**.
3. Click **Add rule**: type **HTTP**, source **Anywhere-IPv4**. Add another: type **HTTPS**, source **Anywhere-IPv4**.
4. Click **Save rules**.

With the AWS CLI ([authorize-security-group-ingress](https://docs.aws.amazon.com/cli/latest/reference/ec2/authorize-security-group-ingress.html)):

```bash
aws ec2 authorize-security-group-ingress --group-id <security-group-id> --ip-permissions \
	"IpProtocol=tcp,FromPort=80,ToPort=80,IpRanges=[{CidrIp=0.0.0.0/0}]" \
	"IpProtocol=tcp,FromPort=443,ToPort=443,IpRanges=[{CidrIp=0.0.0.0/0}]"
```

If the subnet has a custom network ACL, it must allow 80 and 443 in and ephemeral ports out ([network ACLs](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-network-acls.html)). The default network ACL allows everything. Lightsail instances have their own firewall on the instance's **Networking** tab.

## Google Cloud (Compute Engine)

Every VPC network denies inbound traffic unless a firewall rule allows it, and the default network's rules only allow SSH, RDP, ICMP and internal traffic ([VPC firewall rules](https://cloud.google.com/firewall/docs/firewalls)).

1. In the Compute Engine console, open **VM instances**, click the server, then **Edit**.
2. Under **Firewalls**, tick **Allow HTTP traffic** and **Allow HTTPS traffic**.
3. Click **Save**.

On a network other than `default`, or with the gcloud CLI, create a rule and tag the server ([firewall-rules create](https://cloud.google.com/sdk/gcloud/reference/compute/firewall-rules/create)):

```bash
gcloud compute firewall-rules create tero-web --network=<network> --allow=tcp:80,tcp:443 --source-ranges=0.0.0.0/0 --target-tags=tero
gcloud compute instances add-tags <instance> --zone=<zone> --tags=tero
```

## Azure

A network security group denies inbound traffic from the internet unless a rule allows it (`DenyAllInBound`, [network security groups](https://learn.microsoft.com/azure/virtual-network/network-security-groups-overview)).

1. In the Azure portal, open the virtual machine, then **Networking** > **Network settings**.
2. Click **Create port rule** > **Inbound port rule**.
3. Destination port ranges `80,443`, protocol **TCP**, action **Allow**.
4. Click **Add**.

With the Azure CLI ([az vm open-port](https://learn.microsoft.com/cli/azure/vm#az-vm-open-port)):

```bash
az vm open-port --resource-group <group> --name <vm> --port 80 --priority 1010
az vm open-port --resource-group <group> --name <vm> --port 443 --priority 1020
```

## Oracle Cloud

The default security list of a VCN only lets in SSH and some ICMP ([security lists](https://docs.oracle.com/iaas/Content/Network/Concepts/securitylists.htm)).

1. In the OCI console, open the instance and click its subnet.
2. Under **Security**, open the security list the subnet uses (by default, **Default Security List for** your VCN).
3. Click **Add Ingress Rules**: source CIDR `0.0.0.0/0`, IP protocol **TCP**, destination port range `80`. Add another for `443`.
4. Click **Add Ingress Rules**.

If the instance uses network security groups, add the same rules there.

Oracle's Ubuntu images also ship iptables rules on the server itself that reject everything but SSH ([Oracle's guide](https://blogs.oracle.com/developers/post/enabling-network-traffic-to-ubuntu-images-in-oracle-cloud-infrastructure)), and Tero's firewall doesn't remove them. Open the ports there too:

```bash
sudo iptables -I INPUT -p tcp -m multiport --dports 80,443 -m conntrack --ctstate NEW -j ACCEPT
sudo netfilter-persistent save
```

## Other providers

DigitalOcean, Hetzner and Linode (Akamai) leave ports open unless you attach a cloud firewall to the server. If you did, add inbound rules for TCP 80 and 443 from all IPv4 and IPv6 addresses.

A firewall on the server itself can also block the ports, even though Tero's own firewall opens them: nftables drops a packet when any of its tables drops it. Some providers' images turn on ufw, for example. Check with `sudo ufw status` and `sudo nft list ruleset`, and allow TCP 80 and 443 there (`sudo ufw allow 80,443/tcp`).
