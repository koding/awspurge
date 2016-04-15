package awspurge

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/hashicorp/go-multierror"
)

func (p *Purge) fetchELBResources(fn func(*elb.ELB) error) {
	for _, s := range p.services.elb {
		p.fetchWg.Add(1)

		go func(svc *elb.ELB) {
			err := fn(svc)
			if err != nil {
				p.fetchMu.Lock()
				p.fetchErrs = multierror.Append(p.fetchErrs, err)
				p.fetchMu.Unlock()
			}

			p.fetchWg.Done()
		}(s)
	}
}

func (p *Purge) fetchEC2Resources(fn func(*ec2.EC2) error) {
	for _, s := range p.services.ec2 {
		p.fetchWg.Add(1)

		go func(svc *ec2.EC2) {
			err := fn(svc)
			if err != nil {
				p.fetchMu.Lock()
				p.fetchErrs = multierror.Append(p.fetchErrs, err)
				p.fetchMu.Unlock()
			}

			p.fetchWg.Done()
		}(s)
	}
}

func (p *Purge) FetchInstances() {
	fn := func(svc *ec2.EC2) error {
		region := *svc.Config.Region
		params := &ec2.DescribeInstancesInput{
			Filters: []*ec2.Filter{p.getVpcIdFilter(region, "vpc-id")},
		}

		resp, err := svc.DescribeInstances(params)
		if err != nil {
			return err
		}

		instances := make([]*ec2.Instance, 0)
		if resp.Reservations != nil {
			for _, reserv := range resp.Reservations {
				if len(reserv.Instances) == 0 {
					continue
				}

				filters := p.filters.Instance
				if len(filters) == 0 {
					instances = append(instances, reserv.Instances...)
				}
				for _, instance := range reserv.Instances {
					match := true
					for _, filter := range filters {
						if !filter(instance) {
							match = false
							break
						}
					}
					if match {
						instances = append(instances, instance)
					}
				}
			}
		}

		p.resourceMu.Lock()
		p.resources[region].instances = instances
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchVolumes() {
	fn := func(svc *ec2.EC2) error {
		region := *svc.Config.Region
		params := &ec2.DescribeVolumesInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("attachment.instance-id"),
					Values: p.getInstanceIds(region),
				},
			},
		}

		resp, err := svc.DescribeVolumes(params)
		if err != nil {
			return err
		}

		volumes := resp.Volumes

		p.resourceMu.Lock()
		p.resources[region].volumes = volumes
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchKeyPairs() {
	fn := func(svc *ec2.EC2) error {
		resp, err := svc.DescribeKeyPairs(nil)
		if err != nil {
			return err
		}

		var resources []*ec2.KeyPairInfo
		region := *svc.Config.Region

		filters := p.filters.KeyPair
		if len(filters) == 0 {
			resources = resp.KeyPairs
		} else {
			for _, keyPair := range resp.KeyPairs {
				match := true
				for _, filter := range filters {
					if !filter(keyPair) {
						match = false
						break
					}
				}
				if match {
					resources = append(resources, keyPair)
				}
			}
		}

		p.resourceMu.Lock()
		p.resources[region].keyPairs = resources
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchPlacementGroups() {
	fn := func(svc *ec2.EC2) error {
		resp, err := svc.DescribePlacementGroups(nil)
		if err != nil {
			return err
		}

		resources := resp.PlacementGroups
		region := *svc.Config.Region

		p.resourceMu.Lock()
		p.resources[region].placementGroups = resources
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchAddresses() {
	fn := func(svc *ec2.EC2) error {
		region := *svc.Config.Region
		params := &ec2.DescribeAddressesInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("instance-id"),
					Values: p.getInstanceIds(region),
				},
			},
		}

		resp, err := svc.DescribeAddresses(params)
		if err != nil {
			return err
		}

		resources := resp.Addresses

		p.resourceMu.Lock()
		p.resources[region].addresses = resources
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchSnapshots() {
	fn := func(svc *ec2.EC2) error {
		input := &ec2.DescribeSnapshotsInput{
			OwnerIds: stringSlice("self"),
		}

		resp, err := svc.DescribeSnapshots(input)
		if err != nil {
			return err
		}

		resources := resp.Snapshots
		region := *svc.Config.Region

		p.resourceMu.Lock()
		p.resources[region].snapshots = resources
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchSecurityGroups() {
	fn := func(svc *ec2.EC2) error {
		region := *svc.Config.Region
		params := &ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{p.getVpcIdFilter(region, "vpc-id")},
		}

		resp, err := svc.DescribeSecurityGroups(params)
		if err != nil {
			return err
		}

		resources := resp.SecurityGroups

		p.resourceMu.Lock()
		p.resources[region].securityGroups = resources
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchLoadBalancers() {
	fn := func(svc *elb.ELB) error {
		resp, err := svc.DescribeLoadBalancers(nil)
		if err != nil {
			return err
		}

		loadBalancers := resp.LoadBalancerDescriptions
		region := *svc.Config.Region

		p.resourceMu.Lock()
		p.resources[region].loadBalancers = loadBalancers
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchELBResources(fn)
}

func (p *Purge) FetchVpcs() {
	fn := func(svc *ec2.EC2) error {
		resp, err := svc.DescribeVpcs(nil)
		if err != nil {
			return err
		}

		region := *svc.Config.Region
		filters := p.filters.Vpc

		vpcs := make([]*ec2.Vpc, 0)
		for _, vpc := range resp.Vpcs {
			// don't delete default vpc
			if *vpc.IsDefault {
				continue
			}

			if len(filters) == 0 {
				vpcs = append(vpcs, vpc)
			} else {
				match := true
				for _, filter := range filters {
					if !filter(vpc) {
						match = false
						break
					}
				}
				if match {
					vpcs = append(vpcs, vpc)
				}
			}
		}

		p.resourceMu.Lock()
		p.resources[region].vpcs = vpcs
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchSubnets() {
	fn := func(svc *ec2.EC2) error {
		region := *svc.Config.Region
		params := &ec2.DescribeSubnetsInput{
			Filters: []*ec2.Filter{p.getVpcIdFilter(region, "vpc-id")},
		}

		resp, err := svc.DescribeSubnets(params)
		if err != nil {
			return err
		}

		resources := resp.Subnets

		p.resourceMu.Lock()
		p.resources[region].subnets = resources
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchNetworkAcls() {
	fn := func(svc *ec2.EC2) error {
		region := *svc.Config.Region
		params := &ec2.DescribeNetworkAclsInput{
			Filters: []*ec2.Filter{p.getSubnetIdFilter(region, "association.subnet-id")},
		}

		resp, err := svc.DescribeNetworkAcls(params)
		if err != nil {
			return err
		}

		resources := resp.NetworkAcls

		p.resourceMu.Lock()
		p.resources[region].networkAcls = resources
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchInternetGateways() {
	fn := func(svc *ec2.EC2) error {
		region := *svc.Config.Region
		params := &ec2.DescribeInternetGatewaysInput{
			Filters: []*ec2.Filter{p.getVpcIdFilter(region, "attachment.vpc-id")},
		}

		resp, err := svc.DescribeInternetGateways(params)
		if err != nil {
			return err
		}

		resources := resp.InternetGateways

		p.resourceMu.Lock()
		p.resources[region].internetGateways = resources
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) FetchRouteTables() {
	fn := func(svc *ec2.EC2) error {
		region := *svc.Config.Region
		params := &ec2.DescribeRouteTablesInput{
			Filters: []*ec2.Filter{p.getSubnetIdFilter(region, "association.subnet-id")},
		}

		resp, err := svc.DescribeRouteTables(params)
		if err != nil {
			return err
		}

		resources := resp.RouteTables

		p.resourceMu.Lock()
		p.resources[region].routeTables = resources
		p.resourceMu.Unlock()
		return nil
	}

	p.fetchEC2Resources(fn)
}

func (p *Purge) getVpcIdFilter(region string, name string) *ec2.Filter {
	filters := &ec2.Filter{
		Name:   aws.String(name),
		Values: []*string{},
	}

	for _, vpc := range p.resources[region].vpcs {
		filters.Values = append(filters.Values, vpc.VpcId)
	}

	return filters
}

func (p *Purge) getSubnetIdFilter(region string, name string) *ec2.Filter {
	filters := &ec2.Filter{
		Name:   aws.String(name),
		Values: []*string{},
	}

	for _, subnet := range p.resources[region].subnets {
		filters.Values = append(filters.Values, subnet.SubnetId)
	}

	return filters
}

func (p *Purge) getInstanceIds(region string) (ids []*string) {
	for _, instance := range p.resources[region].instances {
		ids = append(ids, instance.InstanceId)
	}
	return ids
}

// stringSlice is an helper method to convert a slice of strings into a slice
// of pointer of strings. Needed for various aws/ec2 commands.
func stringSlice(vals ...string) []*string {
	a := make([]*string, len(vals))

	for i, v := range vals {
		a[i] = aws.String(v)
	}

	return a
}
