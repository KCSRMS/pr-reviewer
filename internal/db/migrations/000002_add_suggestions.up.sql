ALTER TABLE public.review_comments
    ADD COLUMN start_line bigint DEFAULT 0 NOT NULL,
    ADD COLUMN suggestion text DEFAULT ''::text NOT NULL;
