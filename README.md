# Geo-Index Based Social Network
**What I have finished**: backend of the social network  
**What I will do in the future**: frontend of the social network

# Demo
![image](https://github.com/YIYUAN-REN/Geo-Index-Based-Social-Network/blob/master/Demo.gif)

# Overview
•	Developed a web service (Go) to handle post events, search nearby posts, sign up, and log in.  
•	Used Google Dataflow to dump posts from BigTable to BigQuery table (Java) for offline analysis.  
•	Deployed the web service to Google App Engine for better scaling.  
•	Utilized Elasticsearch in Google Compute Engine to provide storage and geo-location based search functions to search posts.  
•	Used Google Cloud Storage to store images that were posted by users.  
•	Improved keyword-based spam detection by aggregated the data at the post level and user level.  

# Architecture
![image](https://github.com/YIYUAN-REN/Geo-Index-Based-Social-Network/blob/master/Architecture.png)
