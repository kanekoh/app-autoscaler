package org.cloudfoundry.autoscaler.scheduler.quartz;

import java.util.Date;

import org.apache.logging.log4j.LogManager;
import org.apache.logging.log4j.Logger;
import org.cloudfoundry.autoscaler.scheduler.dao.ActiveScheduleDao;
import org.cloudfoundry.autoscaler.scheduler.entity.ActiveScheduleEntity;
import org.cloudfoundry.autoscaler.scheduler.util.JobActionEnum;
import org.cloudfoundry.autoscaler.scheduler.util.ScheduleJobHelper;
import org.cloudfoundry.autoscaler.scheduler.util.error.MessageBundleResourceHelper;
import org.quartz.JobDataMap;
import org.quartz.JobExecutionContext;
import org.quartz.Scheduler;
import org.quartz.SchedulerException;
import org.quartz.Trigger;
import org.quartz.TriggerKey;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.http.HttpEntity;
import org.springframework.http.HttpStatus;
import org.springframework.scheduling.quartz.QuartzJobBean;
import org.springframework.stereotype.Component;
import org.springframework.web.client.HttpStatusCodeException;
import org.springframework.web.client.ResourceAccessException;
import org.springframework.web.client.RestTemplate;

/**
 * QuartzJobBean class that executes the job
 */
@Component
abstract class AppScalingScheduleJob extends QuartzJobBean {
	private Logger logger = LogManager.getLogger(this.getClass());

	@Value("${autoscaler.scalingengine.url}")
	private String scalingEngineUrl;

	@Value("${scalingenginejob.reschedule.interval.millisecond}")
	long jobRescheduleIntervalMilliSecond;

	@Value("${scalingenginejob.reschedule.maxcount}")
	int maxJobRescheduleCount;

	@Value("${scalingengine.notification.reschedule.maxcount}")
	int maxScalingEngineNotificationRescheduleCount;

	@Autowired
	ActiveScheduleDao activeScheduleDao;

	@Autowired
	private RestTemplate restTemplate;

	@Autowired
	MessageBundleResourceHelper messageBundleResourceHelper;

	void notifyScalingEngine(ActiveScheduleEntity activeScheduleEntity, JobActionEnum scalingAction,
			JobExecutionContext jobExecutionContext) {
		String appId = activeScheduleEntity.getAppId();
		Long scheduleId = activeScheduleEntity.getId();
		HttpEntity<ActiveScheduleEntity> requestEntity = new HttpEntity<>(activeScheduleEntity);

		try {
			String scalingEnginePathActiveSchedule = scalingEngineUrl + "/v1/apps/" + appId + "/active_schedules/" + scheduleId;

			if (scalingAction == JobActionEnum.START) {
				String message = messageBundleResourceHelper.lookupMessage("scalingengine.notification.activeschedule.start", appId,
						scheduleId, scalingAction);
				logger.info(message);
				restTemplate.put(scalingEnginePathActiveSchedule, requestEntity);
			} else {
				String message = messageBundleResourceHelper.lookupMessage("scalingengine.notification.activeschedule.remove", appId,
						scheduleId, scalingAction);
				logger.info(message);
				restTemplate.delete(scalingEnginePathActiveSchedule, requestEntity);
			}
		} catch (HttpStatusCodeException hce) {
			handleResponse(activeScheduleEntity, scalingAction, hce);
		} catch (ResourceAccessException rae) {
			String message = messageBundleResourceHelper.lookupMessage("scalingengine.notification.error",
					rae.getMessage(), appId, scheduleId, scalingAction);
			logger.error(message, rae);
			handleJobRescheduling(jobExecutionContext, ScheduleJobHelper.RescheduleCount.SCALING_ENGINE_NOTIFICATION,
					maxScalingEngineNotificationRescheduleCount);
		}
	}

	private void handleResponse(ActiveScheduleEntity activeScheduleEntity, JobActionEnum scalingAction,
			HttpStatusCodeException hsce) {
		String appId = activeScheduleEntity.getAppId();
		Long scheduleId = activeScheduleEntity.getId();
		HttpStatus errorResponseCode = hsce.getStatusCode();
		if (errorResponseCode.is4xxClientError()) {
			String message = messageBundleResourceHelper.lookupMessage("scalingengine.notification.client.error",
					errorResponseCode, hsce.getResponseBodyAsString(), appId, scheduleId, scalingAction);
			logger.error(message, hsce);
		} else {
			String message = messageBundleResourceHelper.lookupMessage("scalingengine.notification.failed",
					errorResponseCode, hsce.getResponseBodyAsString(), appId, scheduleId, scalingAction);
			logger.error(message, hsce);
		}
	}

	void handleJobRescheduling(JobExecutionContext jobExecutionContext, ScheduleJobHelper.RescheduleCount retryCounter,
			int maxCount) {
		JobDataMap jobDataMap = jobExecutionContext.getJobDetail().getJobDataMap();
		String retryCounterTask = retryCounter.name();// ACTIVE_SCHEDULE, SCALING_ENGINE_NOTIFICATION
		int jobFireCount = jobDataMap.getInt(retryCounterTask);
		String appId = jobDataMap.getString(ScheduleJobHelper.APP_ID);
		Long scheduleId = jobDataMap.getLong(ScheduleJobHelper.SCHEDULE_ID);
		TriggerKey triggerKey = jobExecutionContext.getTrigger().getKey();

		if (jobFireCount < maxCount) {
			Date newTriggerTime = new Date(System.currentTimeMillis() + jobRescheduleIntervalMilliSecond);
			Trigger newTrigger = ScheduleJobHelper.buildTrigger(triggerKey, null, newTriggerTime);

			try {
				Scheduler scheduler = jobExecutionContext.getScheduler();
				jobDataMap.put(retryCounterTask, ++jobFireCount);
				scheduler.addJob(jobExecutionContext.getJobDetail(), true);
				scheduler.rescheduleJob(triggerKey, newTrigger);
			} catch (SchedulerException se) {
				String errorMessage = messageBundleResourceHelper.lookupMessage("scheduler.job.reschedule.failed",
						se.getMessage(), triggerKey, appId, scheduleId, jobFireCount - 1);
				logger.error(errorMessage, se);
			}
		} else {
			String errorMessage = messageBundleResourceHelper.lookupMessage(
					"scheduler.job.reschedule.failed.max.reached", triggerKey, appId, scheduleId, maxCount,
					retryCounterTask);
			logger.error(errorMessage);
		}
	}
}
