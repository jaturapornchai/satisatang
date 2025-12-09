# Deploy Satisatang to Vercel

This guide will help you deploy the Satisatang Line bot to Vercel as serverless functions.

## Prerequisites

- Vercel account (sign up free at [vercel.com](https://vercel.com))
- Git repository (locally committed code)
- All required API credentials

## Step 1: Install Vercel CLI

```powershell
npm i -g vercel
```

## Step 2: Login to Vercel

```powershell
vercel login
```

Follow the authentication prompts.

## Step 3: Deploy to Preview

From your project directory:

```powershell
vercel
```

This creates a preview deployment. Answer the prompts:
- **Set up and deploy?** Yes
- **Which scope?** Choose your account
- **Link to existing project?** No (first time) or Yes (subsequent deploys)
- **Project name?** satisatang (or your preferred name)
- **In which directory is your code located?** ./
- **Override settings?** No

## Step 4: Configure Environment Variables

Go to your Vercel dashboard → Project Settings → Environment Variables.

Add the following variables (copy from your `.env` file):

| Variable Name | Description |
|--------------|-------------|
| `LINE_CHANNEL_SECRET` | Line channel secret from LINE Developers Console |
| `LINE_CHANNEL_ACCESS_TOKEN` | Line channel access token |
| `GEMINI_API_KEY` | Google Gemini API key |
| `GEMINI_MODEL` | Model name (default: `gemini-2.5-flash-lite`) |
| `MONGODB_ATLAS_URI` | MongoDB Atlas connection string |
| `MONGODB_ATLAS_DBNAME` | Database name (default: `satistang`) |
| `FIREBASE_CREDENTIALS` | Firebase service account JSON (optional) |
| `FIREBASE_STORAGE_BUCKET` | Firebase storage bucket name (optional) |

**Important:** Make sure to add these to **Production**, **Preview**, and **Development** environments.

## Step 5: Redeploy with Environment Variables

```powershell
vercel --prod
```

This deploys to production with your environment variables.

## Step 6: Update Line Webhook URL

1. Go to [LINE Developers Console](https://developers.line.biz/console/)
2. Select your channel
3. Go to **Messaging API** tab
4. Update **Webhook URL** to:
   ```
   https://your-project-name.vercel.app/webhook/line
   ```
5. Enable **Use webhook**
6. Click **Verify** to test the webhook

## Step 7: Test Your Deployment

### Test Health Endpoint

```powershell
curl https://your-project-name.vercel.app/health
```

Expected response:
```json
{"service":"satisatang","status":"ok"}
```

### Test Line Bot

Send a message to your Line bot. It should respond as expected.

## View Logs

To view function logs in real-time:

```powershell
vercel logs your-project-name --follow
```

Or view in Vercel dashboard → Project → Deployments → [Select deployment] → Functions

## Troubleshooting

### Cold Start Issues
- Serverless functions may have cold starts (1-3 seconds delay)
- This is normal for Vercel Go functions
- Connections are reused when possible

### Environment Variables Not Working
- Make sure variables are set for the correct environment
- Redeploy after adding variables: `vercel --prod`

### Webhook Verification Failed
- Check that the URL ends with `/webhook/line`
- Ensure `LINE_CHANNEL_SECRET` is correctly set
- View function logs for error messages

### MongoDB Connection Issues
- Verify `MONGODB_ATLAS_URI` format
- Ensure your MongoDB Atlas allows connections from 0.0.0.0/0
- Check database name matches `MONGODB_ATLAS_DBNAME`

## Continuous Deployment

Link your Git repository for automatic deployments:

1. In Vercel dashboard → Project Settings → Git
2. Connect your GitHub/GitLab/Bitbucket repository
3. Every push to `main` branch will trigger deployment

## Commands Reference

```powershell
# Deploy to preview
vercel

# Deploy to production
vercel --prod

# View logs
vercel logs

# List deployments
vercel ls

# Remove deployment
vercel rm deployment-url
```
