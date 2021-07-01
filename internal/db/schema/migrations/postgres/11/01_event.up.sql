begin;

/*

┌─────────────────┐                                  
│iam_scope_global │                                  
├─────────────────┤                                  
│                 │                                  
└─────────────────┘                                  
         ┼                                           
         │                                           
         ┼                                           
         ┼                                           
┌─────────────────┐                                  
│  event_config   │           ┌──────────────────┐   
├─────────────────┤           │event_type_enabled│   
│ public_id       │          ╱├──────────────────┤   
│ scope_id        │┼────────○─│config_id         │   
│                 │          ╲│event_type        │   
│                 │           │                  │   
└─────────────────┘           └──────────────────┘   
         ┼                              ┼            
         ┼                              │            
         │                              │            
         │                             ╱│╲           
         ○                     ┌─────────────────┐   
        ╱│╲                   ╱│ event_type_enm  │   
┌─────────────────┐     ┌──────├─────────────────┤   
│   event_sink    │     │     ╲│                 │   
├─────────────────┤     │      └─────────────────┘   
│public_id        │┼────┤                            
│config_id        │     │     ┌─────────────────────┐
│                 │     │    ╱│ event_sink_type_enm │
└─────────────────┘     ├─────├─────────────────────┤
         ┼              │    ╲│                     │
         │              │     └─────────────────────┘
         │              │     ┌─────────────────┐    
         ○              │    ╱│event_format_type│    
        ╱│╲             └─────├─────────────────┤    
┌─────────────────┐          ╲│                 │    
│ event_file_sink │           └─────────────────┘    
├─────────────────┤                                  
│ public_id       │                                  
│ sink_id         │                                  
│ path            │                                  
│ file_name       │                                  
│ rotate_bytes    │                                  
│ rotate_duration │                                  
│ rotate_max_files│                                  
└─────────────────┘                                                                                      

*/

create table event_type_enm (
    name text primary key
        constraint only_predefined_event_types_allowed
        check (
            name in (
                'every',
                'error',
                'audit',
                'observation',
                'system'
            )
        )
);
comment on table event_type_enm is
'event_type_enm is an enumeration table for the valid event types within the '
'the domain';

create table event_sink_type_enm (
    name text primary key
        constraint only_predefined_event_sink_types_allowed
        check (
            name in (
                'stderr',
                'file'
            )
        )
);
comment on table event_type_enm is
'event_sink_type_enm is an enumeration table for the valid sink types within the '
'the domain';

create table event_format_type_enm (
    name text primary key
        constraint only_predefined_event_format_types_allowed
        check (
            name in (
                'json',
                'text'
            )
        )
);
comment on table event_format_type_enm is
'event_format_type_enm is an enumeration table for the valid event format types'
'within the domain';

create table event_config (
    public_id wt_public_id primary key,
    scope_id wt_scope_id not null
        constraint iam_scope_global_fkey
            references iam_scope_global(scope_id)
            on delete cascade
            on update cascade,
    constraint scope_id_uq -- only allow one config per scope
        unique (scope_id),   
    name wt_name,
    description wt_description,
    create_time wt_timestamp,
    update_time wt_timestamp
);
comment on table event_config is
'event_config is a table where each entry defines the event configuration for '
'a scope.  Currently, the only support scope is global';

create table event_type_enabled (
    config_id wt_public_id
        constraint event_config_fkey
            references event_config(public_id)
            on delete cascade
            on update cascade,
    event_type text not null
        constraint event_type_enm_fkey
            references event_type_enm (name)
            on delete restrict
            on update cascade,
    constraint config_id_event_type_uq
        unique (config_id, event_type) -- only allow an event type to be enable once
);
comment on table event_type_enabled is
'event_type_enable is a table where each entry represents that eventing has '
'been enabled for the specified event type in an event configuration';


create table event_sink(
    public_id wt_public_id primary key,
    config_id wt_public_id 
        constraint event_config_fkey
        references event_config(public_id)
        on delete cascade
        on update cascade,
    event_type text not null
        constraint event_type_enm_fkey
        references event_type_enm(name)
        on delete restrict
        on update cascade,
    sink_type text not null
        constraint event_sink_type_enm_fkey
        references event_sink_type_enm(name)
        on delete restrict
        on update cascade,
    format_type text not null
        constraint event_format_type_enm_fkey
        references event_format_type_enm(name)
        on delete restrict
        on update cascade
);
comment on table event_sink is 
'event_sink is a table where each entry represents a configured event sink';

create table event_file_sink(
    public_id wt_public_id primary key,
    sink_id wt_public_id not null
        constraint event_sink_type_fkey
            references event_sink(public_id)
            on delete cascade
            on update cascade,
    path text not null 
        constraint path_not_empty
        check (
            length(trim(path)) > 0
        ),
    filename text not null
        constraint filename_not_empty
        check (
            length(trim(filename)) > 0
        ),
    rotate_bytes int 
        constraint rotate_bytes_null_or_greater_than_zero
        check(
            rotate_bytes is null 
                or 
            rotate_bytes > 0
        ),
    rotate_duration interval
        constraint rotate_duration_null_or_greater_than_zero
        check(
            rotate_duration is null 
                or 
            rotate_duration > '0'::interval
        ),
    rotate_max_files int 
        constraint rotate_max_files_null_or_greater_than_zero
        check(
            rotate_max_files is null 
                or 
            rotate_max_files > 0
        ),
    constraint path_filename_uq
        unique(path, filename) -- ensure each sink is writing to a unique file
);
comment on table event_file_sink is 
'event_file_sink is a table where each entry represents a configured event file '
' sink';


commit;
