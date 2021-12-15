begin;
create table session_valid_state(
    prior_state text
        references session_state_enm(name)
            on delete restrict
            on update cascade,
        constraint valid_prior_states
        check (
                prior_state in ('pending', 'active', 'canceling')
            ),
    current_state text
        references session_state_enm(name)
            on delete restrict
            on update cascade,
        constraint valid_current_states
        check (
            current_state in ('pending', 'active', 'canceling', 'terminated')
        ),
    primary key (prior_state, current_state)
);

insert into session_valid_state (prior_state, current_state)
values
    ('pending','pending'),
    ('pending','active'),
    ('pending','terminated'),
    ('pending','canceling'),
    ('active','canceling'),
    ('active','terminated'),
    ('canceling','terminated');

alter table session_state
    add column prior_state text not null default 'pending'
        references session_state_enm(name)
        on delete restrict
        on update cascade;
alter table session_state
    add foreign key (prior_state, state)
      references session_valid_state (prior_state,current_state);
alter table session_state
    add unique (session_id, state);

create or replace function
    update_prior_session_state()
    returns trigger
as $$
begin
    -- Prior state is the most recent valid prior state entry for this session_id
    new.prior_state = query.state from(
      select state from session_state where session_id=new.session_id and state in(
          select prior_state from session_valid_state where current_state=new.state )
      order by start_time desc limit 1) as query;

    if new.prior_state is null then
        new.prior_state='pending';
    end if;

    return new;

end;
$$ language plpgsql;

create trigger update_session_state before insert on session_state
    for each row execute procedure update_prior_session_state();
commit;
