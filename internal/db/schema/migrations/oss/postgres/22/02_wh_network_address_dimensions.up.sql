begin;

  -- replaces view from 15/02_wh_rename_host_dimension_org.up.sql
  -- adds the network_address_group_key and removes the host_address column
  drop view whx_host_dimension_target;
  create view whx_host_dimension_target as
  select key,
         network_address_group_key,
         host_id,
         host_type,
         host_name,
         host_description,
         host_address,
         host_set_id,
         host_set_type,
         host_set_name,
         host_set_description,
         host_catalog_id,
         host_catalog_type,
         host_catalog_name,
         host_catalog_description,
         target_id,
         target_type,
         target_name,
         target_description,
         target_default_port_number,
         target_session_max_seconds,
         target_session_connection_limit,
         project_id,
         project_name,
         project_description,
         organization_id,
         organization_name,
         organization_description
  from wh_host_dimension
  where current_row_indicator = 'Current'
  ;

  -- wh_private_address_status returns a warehouse appropriate string
  -- representing if the address is private, public, or not applicable for the
  -- provided address.  An address which cannot be cast to an inet results in
  -- 'Not Applicable' being returned.
  create function wh_private_address_status(inet) returns text
  as $$
  begin
    case
      when $1 << any ('{10.0.0.0/8, 192.168.0.0/16, 172.16.0.0/12, fc00::/7, fe80::/10}'::cidr[]) then
        return 'Private';
      else
        return 'Public';
      end case;
  end;
  $$ language plpgsql;

  create view whx_network_address_dimension_source as
    select
      hdns.host_id as host_id,
      hdns.name as address,
      'DNS Name' as address_type,
      'Not Applicable' as ip_address_family,
      'Not Applicable' private_ip_address_status,
      hdns.name as dns_name,
      'Not Applicable' as ip4_address,
      'Not Applicable' as ip6_address
    from host_dns_name as hdns
    union
    select -- id is the first column in the target view
       hip.host_id as host_id,
       host(hip.address) as address,
       'IP Address' as address_type,
       case
         when family(hip.address) = 4 then 'IPv4'
         when family(hip.address) = 6 then 'IPv6'
         else 'Not Applicable'
         end               as ip_address_family,
       wh_private_address_status(hip.address) as private_ip_address_status,
       'Not Applicable' as dns_name,
       case
         when hip.address is not null and family(hip.address) = 4 then host(hip.address)
         else 'Not Applicable'
         end as ip4_address,
       case
         when hip.address is not null and family(hip.address) = 6 then host(hip.address)
         else 'Not Applicable'
         end as ip6_address
    from host_ip_address as hip;


  -- replaces view from 20/07_wh_session_dimensions.up.sql

  -- Updates whx_host_dimension_source to support plugin based host and related
  -- resources.  While this will return rows where there are host_sets where hosts
  -- are not in the host sets, this view is only used when looking up a row by
  -- host, set, and target so the records where hosts and host sets aren't
  -- related beyond the shared catalog will be ignored.  Retrieving the ip address
  -- or dns name of plugin based hosts is not supported in this version of the
  -- warehouse.
  drop view whx_host_dimension_source;
  create view whx_host_dimension_source as
    select -- id is the first column in the target view
         h.public_id                     as host_id,
         case when sh.public_id is not null then 'static host'
              when ph.public_id is not null then 'plugin host'
              else 'Unknown' end          as host_type,
         case when sh.public_id is not null then coalesce(sh.name, 'None')
              when ph.public_id is not null then coalesce(ph.name, 'None')
              else 'Unknown' end          as host_name,
         case when sh.public_id is not null then coalesce(sh.description, 'None')
              when ph.public_id is not null then coalesce(ph.description, 'None')
              else 'Unknown' end          as host_description,

         coalesce(sh.address, 'Unsupported') as host_address,

         hs.public_id                     as host_set_id,
         case when shs.public_id is not null then 'static host set'
              when phs.public_id is not null then 'plugin host set'
              else 'Unknown' end          as host_set_type,
         case
           when shs.public_id is not null then coalesce(shs.name, 'None')
           when phs.public_id is not null then coalesce(phs.name, 'None')
           else 'None'
           end                            as host_set_name,
         case
           when shs.public_id is not null then coalesce(shs.description, 'None')
           when phs.public_id is not null then coalesce(phs.description, 'None')
           else 'None'
           end                            as host_set_description,
         hc.public_id                     as host_catalog_id,
         case when shc.public_id is not null then 'static host catalog'
              when phc.public_id is not null then 'plugin host catalog'
              else 'Unknown' end          as host_catalog_type,
         case
           when shc.public_id is not null then coalesce(shc.name, 'None')
           when phc.public_id is not null then coalesce(phc.name, 'None')
           else 'None'
           end                            as host_catalog_name,
         case
           when shc.public_id is not null then coalesce(shc.description, 'None')
           when phc.public_id is not null then coalesce(phc.description, 'None')
           else 'None'
           end                            as host_catalog_description,
         t.public_id                     as target_id,
         'tcp target'                    as target_type,
         coalesce(t.name, 'None')        as target_name,
         coalesce(t.description, 'None') as target_description,
         coalesce(t.default_port, 0)     as target_default_port_number,
         t.session_max_seconds           as target_session_max_seconds,
         t.session_connection_limit      as target_session_connection_limit,
         p.public_id                     as project_id,
         coalesce(p.name, 'None')        as project_name,
         coalesce(p.description, 'None') as project_description,
         o.public_id                     as organization_id,
         coalesce(o.name, 'None')        as organization_name,
         coalesce(o.description, 'None') as organization_description
    from host as h
         join host_catalog as hc                on h.catalog_id = hc.public_id
         join host_set as hs                    on h.catalog_id = hs.catalog_id
         join target_host_set as ts             on hs.public_id = ts.host_set_id
         join target_tcp as t                   on ts.target_id = t.public_id
         join iam_scope as p                    on t.scope_id = p.public_id and p.type = 'project'
         join iam_scope as o                    on p.parent_id = o.public_id and o.type = 'org'

         left join static_host as sh            on sh.public_id = h.public_id
         left join host_plugin_host as ph       on ph.public_id = h.public_id
         left join static_host_catalog as shc   on shc.public_id = hc.public_id
         left join host_plugin_catalog as phc   on phc.public_id = hc.public_id
         left join static_host_set as shs       on shs.public_id = hs.public_id
         left join host_plugin_set as phs       on phs.public_id = hs.public_id
    ;

commit;
