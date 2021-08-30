CREATE FUNCTION notify_close_trigger() RETURNS trigger as $trigger$
DECLARE
    rec RECORD;

BEGIN

CASE TG_OP
        WHEN 'UPDATE' THEN
            rec := NEW;
ELSE
            RAISE EXCEPTION 'Unknown TG_OP: "%". Should not occur!', TG_OP;
END CASE;
/*FOREACH column_name IN ARRAY TG_ARGV LOOP
    EXECUTE  format('SELECT $1.%I::TEXT', column_name)
    INTO column_value
    using rec;
    payload_items := array_append(payload_items, '"' || replace(column_name, '"', '\"') || '":"' || replace(column_value, '"', '\"') || '"');
END LOOP;
    payload := ''
                   || '{'
                   || array_to_string(payload_items, ',')
        || '}';*/

    perform pg_notify('close_position_notify', row_to_json(NEW)::text);

return rec;
end;


$trigger$ LANGUAGE plpgsql