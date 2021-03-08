# Parse a XenForo2 thread and archive posts

This uses a mongoDB to store information about last post id, last page number, thread number like:

```
{
"thread_id": number;
"post_id": number;
"page": number;
}
```

Create a DB named `xfarchive`, collection named `stats`.

Export OS variables:

```
CHANNEL_ID
```

