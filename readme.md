# A Telegram Bot that parses a XenForo2 thread and archives posts

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
CHANNEL_ID							: Channel ID to broadcast message
MONGODB_USERNAME				: User to connect to MongoDB
MONGODB_PWD							: Password of the said user
MONGODB_ADDR						: Address to connect MongoDB
NEWS_BOT_SECRET_TOKEN		: Bot token
THREAD_1								: XenForo2 thread. Eg: 'abc-title.1234'
```

Build and Run
